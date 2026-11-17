package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/systemprompt"
)

func TestIsChannelBlocked(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"CodeBuddy chat completion failed with 400: request illegal (code 11128)", true},
		{"unapproved channel", true},
		{"UNAPPROVED CHANNEL", true},
		{"model rate limit (code 6004)", false},
		{"prompt is too long (code 11115)", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isChannelBlocked(errors.New(c.msg)); got != c.want {
			t.Fatalf("isChannelBlocked(%q)=%v want %v", c.msg, got, c.want)
		}
	}
	if isChannelBlocked(nil) {
		t.Fatal("nil 应返回 false")
	}
}

func TestSystemPromptModeDrivesKeepSystem(t *testing.T) {
	svc := New(config.Config{Site: "domestic"}, nil)
	t.Cleanup(func() { _ = svc.Close() })

	svc.SetBehaviorSwitches(BehaviorSwitches{SystemPromptMode: config.SystemPromptModeFold})
	if svc.chatOptionsFromAccount(accounts.Account{Site: "domestic"}, CompleteOptions{}).KeepSystem {
		t.Fatal("fold 模式 KeepSystem 应为 false")
	}
	svc.SetBehaviorSwitches(BehaviorSwitches{SystemPromptMode: config.SystemPromptModeKeep})
	if !svc.chatOptionsFromAccount(accounts.Account{Site: "domestic"}, CompleteOptions{}).KeepSystem {
		t.Fatal("keep 模式 KeepSystem 应为 true")
	}
	svc.SetBehaviorSwitches(BehaviorSwitches{SystemPromptMode: config.SystemPromptModeAuto})
	if !svc.chatOptionsFromAccount(accounts.Account{Site: "domestic"}, CompleteOptions{}).KeepSystem {
		t.Fatal("auto 模式首试 KeepSystem 应为 true")
	}
	// 已降级过的请求必须强制 fold，否则会在 11128 上无限递归。
	svc.SetBehaviorSwitches(BehaviorSwitches{SystemPromptMode: config.SystemPromptModeAuto})
	if svc.chatOptionsFromAccount(accounts.Account{Site: "domestic"}, CompleteOptions{FoldedRetry: true}).KeepSystem {
		t.Fatal("FoldedRetry 必须强制 fold")
	}
}

func TestSystemPromptModeSurvivesOtherSwitchUpdates(t *testing.T) {
	svc := New(config.Config{Site: "domestic"}, nil)
	t.Cleanup(func() { _ = svc.Close() })
	svc.SetBehaviorSwitches(BehaviorSwitches{SystemPromptMode: config.SystemPromptModeKeep})
	// 其它开关整批下发时不带 SystemPromptMode，不能把模式清回 auto。
	svc.SetBehaviorSwitches(BehaviorSwitches{QuotaFloorCredits: 12})
	if got := svc.Config().SystemPromptMode; got != config.SystemPromptModeKeep {
		t.Fatalf("模式被意外重置：%q", got)
	}
}

func TestRecordSystemPromptEventNeverStoresPlaintext(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{Site: "domestic", AccountsPath: filepath.Join(dir, "accounts.json")}, nil)
	t.Cleanup(func() { _ = svc.Close() })

	const secret = "ZQ7X-SECRET-MARKER"
	svc.RecordSystemPromptEvent(systemPromptEventFixture(secret))

	recent := svc.SystemPromptJournal.Recent(10)
	if len(recent) != 1 {
		t.Fatalf("应记录 1 条，实际 %d", len(recent))
	}
	blob := recent[0].Fingerprint + " " + recent[0].ErrorCode + " " + recent[0].Model
	if strings.Contains(blob, secret) {
		t.Fatalf("事件里不得出现 system 原文：%q", blob)
	}
	// 落盘文件同样不能有原文。
	matches, _ := filepath.Glob(filepath.Join(dir, "proxy-systemprompt-*.jsonl"))
	if len(matches) != 1 {
		t.Fatalf("应写 1 个按天文件，实际 %v", matches)
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatalf("落盘文件里出现 system 原文：%s", raw)
	}
}

// claudeCodeMarker 是上游 11128 黑名单里**已被网关收录**的精确整句。
// auto/strip 档会在首试前就把它删掉，所以它只适合用来锁「首试就是干净的」。
const claudeCodeMarker = "You are Claude Code, Anthropic's official CLI for Claude."

// unknownFingerprintMarker 模拟「上游黑名单新增、网关还没收录」的指纹串。
//
// 降级链用例必须用它当触发串：v0.7.6 起 auto 档首试前就会剥掉已知指纹，
// 若仍拿 claudeCodeMarker 当触发串，首试 body 里根本没有它，mock 不会回
// 11128 —— 测到的就不是降级而是普通成功。用未知串才能继续覆盖
// 「首试被拒 -> 折叠重放 -> 成功」这条兜底路径。
const unknownFingerprintMarker = "UPSTREAM-BLOCKED-FINGERPRINT-9F3A2C"

// blockOnceTransport 按**请求体内容**决定是否拦截：只要存在 role==system 且
// content 含 marker（零值 = unknownFingerprintMarker）的消息就回 11128，
// 否则回正常 SSE。折叠后该句被挪进 user 的 <client_instructions>、system
// 换成 canonical，于是不再命中扫描 —— 降级用例由此真正锁住「折叠是让它
// 通过的原因」，而不只是「发生了一次重试」。neverBlock=true 时永不拦截。
// onChat 在每次 chat 请求时触发，用于构造「请求进行中切换模式」的场景。
type blockOnceTransport struct {
	mu         sync.Mutex
	bodies     []string
	neverBlock bool
	marker     string
	onChat     func()
}

// messageContentText 把消息 content 压成可检索文本（字符串或 text 块数组）。
func messageContentText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, part := range v {
			switch p := part.(type) {
			case string:
				b.WriteString(p)
			case map[string]any:
				if text, ok := p["text"].(string); ok {
					b.WriteString(text)
				}
			}
		}
		return b.String()
	default:
		return ""
	}
}

// carriesSystemMarker 判定请求体里是否有带指定指纹串的 system 消息。
func carriesSystemMarker(body []byte, marker string) bool {
	var payload struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	for _, m := range payload.Messages {
		role, _ := m["role"].(string)
		if role == "system" && strings.Contains(messageContentText(m["content"]), marker) {
			return true
		}
	}
	return false
}

func (t *blockOnceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	header := make(http.Header)
	path := req.URL.Path
	if strings.Contains(path, "get-user-resource") {
		header.Set("Content-Type", "application/json")
		return &http.Response{
			StatusCode: http.StatusOK, Status: "OK", Header: header,
			Body: io.NopCloser(strings.NewReader(`{"code":1,"msg":"no fixture"}`)), Request: req,
		}, nil
	}
	if strings.Contains(path, "get-dosage-notify") {
		header.Set("Content-Type", "application/json")
		return &http.Response{
			StatusCode: http.StatusOK, Status: "OK", Header: header,
			Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"dosageNotifyCode":0}}`)), Request: req,
		}, nil
	}
	var raw []byte
	if req.Body != nil {
		raw, _ = io.ReadAll(req.Body)
	}
	t.mu.Lock()
	neverBlock := t.neverBlock
	onChat := t.onChat
	marker := t.marker
	t.bodies = append(t.bodies, string(raw))
	t.mu.Unlock()
	if onChat != nil {
		onChat()
	}
	if marker == "" {
		marker = unknownFingerprintMarker
	}
	if !neverBlock && carriesSystemMarker(raw, marker) {
		header.Set("Content-Type", "application/json")
		return &http.Response{
			StatusCode: http.StatusBadRequest, Status: "Bad Request", Header: header,
			Body: io.NopCloser(strings.NewReader(`{"error":{"message":"request illegal (code 11128)","code":11128}}`)), Request: req,
		}, nil
	}
	header.Set("Content-Type", "text/event-stream")
	return &http.Response{
		StatusCode: http.StatusOK, Status: "OK", Header: header,
		Body: io.NopCloser(strings.NewReader(
			"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")), Request: req,
	}, nil
}

func (t *blockOnceTransport) snapshotBodies() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string{}, t.bodies...)
}

func TestAutoModeDowngradesOn11128(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{
		Site:             "domestic",
		AccountsPath:     filepath.Join(dir, "accounts.json"),
		UsagePath:        filepath.Join(dir, "proxy-usage.json"),
		SystemPromptMode: config.SystemPromptModeAuto,
	}, nil)
	t.Cleanup(func() { _ = svc.Close() })

	if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "a", Site: "domestic", BearerToken: "token-a", Enabled: true,
		AuthStatus: accounts.AuthStatus{UserID: "u-a"},
	})); err != nil {
		t.Fatal(err)
	}
	transport := &blockOnceTransport{}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	const clientSystem = unknownFingerprintMarker
	outcome := &SystemPromptOutcome{}
	_, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto",
		Messages: []map[string]any{
			{"role": "system", "content": clientSystem},
			{"role": "user", "content": "hi"},
		},
		SystemOutcome: outcome,
	})
	if err != nil {
		t.Fatalf("降级后应成功，实际 %v", err)
	}

	bodies := transport.snapshotBodies()
	if len(bodies) != 2 {
		t.Fatalf("应打 2 次上游（keep 失败 + fold 重试），实际 %d", len(bodies))
	}
	// 真正锁住因果：第一次原样带未知指纹 system（正是 mock 拦它的原因），
	// 折叠后不再有带该句的 system 消息（mock 据此放行），重试才成功。
	if !carriesSystemMarker([]byte(bodies[0]), unknownFingerprintMarker) {
		t.Fatalf("第一次应原样带含指纹整句的 system，实际 body=%s", bodies[0])
	}
	if carriesSystemMarker([]byte(bodies[1]), unknownFingerprintMarker) {
		t.Fatalf("折叠后不应再有带指纹整句的 system，实际 body=%s", bodies[1])
	}
	folded := chatBodyMessages(t, bodies[1])
	if len(folded) == 0 || folded[0]["role"] != "system" {
		t.Fatalf("第二次首条应是 system，实际 %v", folded)
	}
	if folded[0]["content"] == clientSystem {
		t.Fatalf("第二次不应再原样带客户端 system：%v", folded[0])
	}
	if !strings.Contains(bodies[1], "client_instructions") {
		t.Fatalf("第二次应把 system 折进 client_instructions，实际 body=%s", bodies[1])
	}

	if outcome.Form != "fold" || !outcome.Degraded {
		t.Fatalf("outcome 应记录 fold+degraded，实际 %+v", outcome)
	}

	events := svc.SystemPromptJournal.Recent(10)
	if len(events) != 2 {
		t.Fatalf("应记 2 条事件（keep/11128 + fold/200），实际 %d：%+v", len(events), events)
	}
	// Recent 最新在前。
	if events[0].Form != "fold" || events[0].Status != http.StatusOK || events[0].Degraded {
		t.Fatalf("最新一条应是 fold/200，实际 %+v", events[0])
	}
	if events[1].Form != "keep" || !events[1].Degraded || events[1].ErrorCode != "11128" {
		t.Fatalf("较早一条应是 keep/11128/degraded，实际 %+v", events[1])
	}
	if events[0].Fingerprint == "" || events[0].Fingerprint != events[1].Fingerprint {
		t.Fatalf("两条事件指纹应一致且非空：%q vs %q", events[0].Fingerprint, events[1].Fingerprint)
	}
	if events[0].SystemLen != len(clientSystem) {
		t.Fatalf("SystemLen=%d want %d", events[0].SystemLen, len(clientSystem))
	}
}

// TestAutoModeStripsKnownFingerprintBeforeFirstAttempt 锁住 v0.7.6 的核心约束：
// 已知指纹必须在**首试前**删掉，不能靠「先吃 400 再折叠」兜底。
//
// 上游按被 11128 拒收的次数累计账号风险分，每个请求都先失败一次不可接受。
// 这里用已收录的 claudeCodeMarker 作 system：transport 默认只拦未知串，
// 因此若实现退回「先直传再重试」，首试会被真实上游拒（而 mock 不拦），
// 断言「只打 1 次 + body 里没有该串 + 无 client_instructions」就会失败。
func TestAutoModeStripsKnownFingerprintBeforeFirstAttempt(t *testing.T) {
	transport := &blockOnceTransport{}
	svc := newModeSnapshotTestService(t, config.SystemPromptModeAuto, transport)

	outcome := &SystemPromptOutcome{}
	_, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto",
		Messages: []map[string]any{
			{"role": "system", "content": claudeCodeMarker},
			{"role": "user", "content": "hi"},
		},
		SystemOutcome: outcome,
	})
	if err != nil {
		t.Fatalf("剥离后应首试成功，实际 %v", err)
	}
	bodies := transport.snapshotBodies()
	if len(bodies) != 1 {
		t.Fatalf("首试就应干净，不得重试；实际打 %d 次", len(bodies))
	}
	if strings.Contains(bodies[0], "Claude Code") {
		t.Fatalf("已知指纹未被剥离: %s", bodies[0])
	}
	if strings.Contains(bodies[0], "client_instructions") {
		t.Fatalf("不应走折叠（出现它就说明首试被拒过）: %s", bodies[0])
	}
	if outcome.Degraded || outcome.Form != "keep" {
		t.Fatalf("outcome 应为 keep 且未降级，实际 %+v", outcome)
	}
	if !outcome.Stripped {
		t.Fatalf("outcome 应记录 Stripped=true，实际 %+v", outcome)
	}
	events := svc.SystemPromptJournal.Recent(10)
	if len(events) != 1 {
		t.Fatalf("应只记 1 条事件（没有失败重试），实际 %d：%+v", len(events), events)
	}
	if events[0].Degraded || events[0].Status != http.StatusOK {
		t.Fatalf("事件应为 200 且未降级，实际 %+v", events[0])
	}
	if !events[0].Stripped {
		t.Fatalf("事件应带 Stripped 标记，实际 %+v", events[0])
	}
}

func TestKeepModeDoesNotRetryOn11128(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{
		Site:             "domestic",
		AccountsPath:     filepath.Join(dir, "accounts.json"),
		SystemPromptMode: config.SystemPromptModeKeep,
	}, nil)
	t.Cleanup(func() { _ = svc.Close() })

	if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "a", Site: "domestic", BearerToken: "token-a", Enabled: true,
		AuthStatus: accounts.AuthStatus{UserID: "u-a"},
	})); err != nil {
		t.Fatal(err)
	}
	transport := &blockOnceTransport{}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	_, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto",
		Messages: []map[string]any{
			{"role": "system", "content": unknownFingerprintMarker},
			{"role": "user", "content": "hi"},
		},
	})
	if err == nil {
		t.Fatal("keep 模式应把 11128 原样返回")
	}
	if got := len(transport.snapshotBodies()); got != 1 {
		t.Fatalf("keep 模式不得重试，实际打了 %d 次", got)
	}
}

func TestFoldModeNeverSendsClientSystem(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{
		Site:             "domestic",
		AccountsPath:     filepath.Join(dir, "accounts.json"),
		SystemPromptMode: config.SystemPromptModeFold,
	}, nil)
	t.Cleanup(func() { _ = svc.Close() })

	if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "a", Site: "domestic", BearerToken: "token-a", Enabled: true,
		AuthStatus: accounts.AuthStatus{UserID: "u-a"},
	})); err != nil {
		t.Fatal(err)
	}
	transport := &blockOnceTransport{neverBlock: true}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	_, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto",
		Messages: []map[string]any{
			{"role": "system", "content": "SYSTEM-RAW-TEXT"},
			{"role": "user", "content": "hi"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	bodies := transport.snapshotBodies()
	if len(bodies) != 1 {
		t.Fatalf("应只打 1 次，实际 %d", len(bodies))
	}
	if strings.Contains(bodies[0], `"role":"system","content":"SYSTEM-RAW-TEXT"`) {
		t.Fatalf("fold 模式不应把客户端 system 原样发出：%s", bodies[0])
	}
	if !strings.Contains(bodies[0], "client_instructions") {
		t.Fatalf("fold 模式应折叠到 <client_instructions>：%s", bodies[0])
	}
}

// newModeSnapshotTestService 构造一个带单账号、可控 HTTP transport 的服务，
// 用于「请求中途切换模式」的快照归因测试。
func newModeSnapshotTestService(t *testing.T, mode string, transport http.RoundTripper) *Service {
	t.Helper()
	dir := t.TempDir()
	svc := New(config.Config{
		Site:             "domestic",
		AccountsPath:     filepath.Join(dir, "accounts.json"),
		UsagePath:        filepath.Join(dir, "proxy-usage.json"),
		SystemPromptMode: mode,
	}, nil)
	t.Cleanup(func() { _ = svc.Close() })
	if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "a", Site: "domestic", BearerToken: "token-a", Enabled: true,
		AuthStatus: accounts.AuthStatus{UserID: "u-a"},
	})); err != nil {
		t.Fatal(err)
	}
	svc.Provider.HTTP = &http.Client{Transport: transport}
	return svc
}

// TestSystemPromptEventModeIsSnapshot 锁住「Event.Mode 是请求入口的模式」：
// 请求进行中管理员把模式切成 fold，事件仍应记请求开始时的 auto。
func TestSystemPromptEventModeIsSnapshot(t *testing.T) {
	transport := &blockOnceTransport{neverBlock: true}
	svc := newModeSnapshotTestService(t, config.SystemPromptModeAuto, transport)
	// 第一次（也是唯一一次）chat 请求发出时把模式切成 fold。
	transport.onChat = func() {
		svc.SetBehaviorSwitches(BehaviorSwitches{SystemPromptMode: config.SystemPromptModeFold})
	}

	_, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto",
		Messages: []map[string]any{
			{"role": "system", "content": unknownFingerprintMarker},
			{"role": "user", "content": "hi"},
		},
	})
	if err != nil {
		t.Fatalf("请求应成功，实际 %v", err)
	}
	if got := svc.Config().SystemPromptMode; got != config.SystemPromptModeFold {
		t.Fatalf("前置校验：请求结束后实时模式应为 fold，实际 %q", got)
	}
	events := svc.SystemPromptJournal.Recent(10)
	if len(events) != 1 {
		t.Fatalf("应记 1 条事件，实际 %d：%+v", len(events), events)
	}
	if events[0].Mode != config.SystemPromptModeAuto {
		t.Fatalf("Event.Mode 应为请求开始时的 auto，实际 %q（实时值已变 fold）", events[0].Mode)
	}
}

// TestSystemPromptDowngradeEventsShareSnapshotMode 锁住降级链两条事件的模式一致：
// 中途切成 fold 后，keep/11128 与 fold/200 两条事件都必须是请求入口的 auto，
// 不能一条 auto 一条 fold。
func TestSystemPromptDowngradeEventsShareSnapshotMode(t *testing.T) {
	transport := &blockOnceTransport{}
	svc := newModeSnapshotTestService(t, config.SystemPromptModeAuto, transport)
	transport.onChat = func() {
		svc.SetBehaviorSwitches(BehaviorSwitches{SystemPromptMode: config.SystemPromptModeFold})
	}

	_, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto",
		Messages: []map[string]any{
			{"role": "system", "content": unknownFingerprintMarker},
			{"role": "user", "content": "hi"},
		},
	})
	if err != nil {
		t.Fatalf("降级后应成功，实际 %v", err)
	}
	events := svc.SystemPromptJournal.Recent(10)
	if len(events) != 2 {
		t.Fatalf("应记 2 条事件，实际 %d：%+v", len(events), events)
	}
	for i, e := range events {
		if e.Mode != config.SystemPromptModeAuto {
			t.Fatalf("第 %d 条事件 Mode 应为入口快照 auto，实际 %q：%+v", i, e.Mode, e)
		}
	}
}

// TestSystemPromptDowngradeUsesSnapshotMode 锁住降级判定也用入口快照：
// 请求以 keep 开始，中途才切成 auto；不应因实时值变成 auto 而触发降级。
func TestSystemPromptDowngradeUsesSnapshotMode(t *testing.T) {
	transport := &blockOnceTransport{}
	svc := newModeSnapshotTestService(t, config.SystemPromptModeKeep, transport)
	transport.onChat = func() {
		svc.SetBehaviorSwitches(BehaviorSwitches{SystemPromptMode: config.SystemPromptModeAuto})
	}

	outcome := &SystemPromptOutcome{}
	_, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto",
		Messages: []map[string]any{
			{"role": "system", "content": unknownFingerprintMarker},
			{"role": "user", "content": "hi"},
		},
		SystemOutcome: outcome,
	})
	if err == nil {
		t.Fatal("keep 入口不应降级，11128 应原样返回")
	}
	if got := len(transport.snapshotBodies()); got != 1 {
		t.Fatalf("入口快照为 keep ⇒ 不得降级重试，实际打了 %d 次", got)
	}
	if outcome.Degraded {
		t.Fatalf("outcome 不应标记 degraded，实际 %+v", outcome)
	}
	events := svc.SystemPromptJournal.Recent(10)
	if len(events) != 1 || events[0].Mode != config.SystemPromptModeKeep {
		t.Fatalf("事件应为单条 keep 模式，实际 %+v", events)
	}
}

// systemPromptEventFixture 构造一条带「原文」标记的事件，用于隐私断言。
func systemPromptEventFixture(secret string) systemprompt.Event {
	return systemprompt.Event{
		At:          time.Now().UnixMilli(),
		RequestID:   "req-fixture",
		Form:        "keep",
		Mode:        "auto",
		Fingerprint: systemprompt.Fingerprint("prefix " + secret + " suffix"),
		SystemLen:   len("prefix " + secret + " suffix"),
		Status:      http.StatusBadGateway,
		ErrorCode:   "11128",
		Model:       "auto",
	}
}

// chatBodyMessages 解析上游 chat 请求体里的 messages。
func chatBodyMessages(t *testing.T, body string) []map[string]any {
	t.Helper()
	var payload struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("请求体不是 JSON：%v\n%s", err, body)
	}
	return payload.Messages
}
