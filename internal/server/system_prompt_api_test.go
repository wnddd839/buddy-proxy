package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/gateway"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/systemprompt"
)

// systemPromptTestServer 造一个最小 Server，并把 .env 指到临时文件，
// 避免测试污染仓库或用户真实配置。
func systemPromptTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	t.Setenv("CODEBUDDY_PROXY_ENV_FILE", envPath)
	t.Setenv("CODEBUDDY_PROXY_SYSTEM_PROMPT_MODE", "")
	srv, _, _ := checkinTestServer(t, []accounts.Account{checkinAccount("a1")})
	return srv
}

func doAdminRequest(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	srv.handleAdminAPIEntry(rec, req)
	return rec
}

func TestSystemPromptModeAPIRoundTrip(t *testing.T) {
	srv := systemPromptTestServer(t)

	rec := doAdminRequest(t, srv, http.MethodGet, "/direct-admin/api/system-prompt", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET 状态码 %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		OK   bool   `json:"ok"`
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Mode != "auto" {
		t.Fatalf("默认应为 auto，实际 %q", got.Mode)
	}

	rec = doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-prompt", `{"mode":"keep"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST 状态码 %d: %s", rec.Code, rec.Body.String())
	}
	rec = doAdminRequest(t, srv, http.MethodGet, "/direct-admin/api/system-prompt", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Mode != "keep" {
		t.Fatalf("切换后应为 keep，实际 %q", got.Mode)
	}
	// 必须落到 env 文件，重启后仍生效。
	raw, err := os.ReadFile(os.Getenv("CODEBUDDY_PROXY_ENV_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "CODEBUDDY_PROXY_SYSTEM_PROMPT_MODE=keep") {
		t.Fatalf(".env 未持久化模式：%s", raw)
	}
}

func TestSystemPromptModeAPIKeepsOtherSwitches(t *testing.T) {
	srv := systemPromptTestServer(t)
	srv.Svc.SetBehaviorSwitches(switchesWith(srv, func(b *gateway.BehaviorSwitches) { b.QuotaFloorCredits = 42 }))

	rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-prompt", `{"mode":"fold"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	cfg := srv.Svc.Config()
	if cfg.QuotaFloorCredits != 42 {
		t.Fatalf("积分下限被清掉：%v", cfg.QuotaFloorCredits)
	}
	if cfg.SystemPromptMode != config.SystemPromptModeFold {
		t.Fatalf("模式未生效：%q", cfg.SystemPromptMode)
	}
}

func TestSystemPromptModeAPIRejectsGarbage(t *testing.T) {
	srv := systemPromptTestServer(t)
	rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-prompt", `{"mode":"nonsense"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法值应 400，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "mode") {
		t.Fatalf("错误信息应提到 mode：%s", rec.Body.String())
	}
	if got := config.NormalizeSystemPromptMode(srv.Svc.Config().SystemPromptMode); got != config.SystemPromptModeAuto {
		t.Fatalf("非法值不得改动当前模式，实际 %q", got)
	}
}

func TestSystemPromptEventsPaging(t *testing.T) {
	srv := systemPromptTestServer(t)
	for i := 0; i < 5; i++ {
		srv.Svc.RecordSystemPromptEvent(systemprompt.Event{
			At: time.Now().UnixMilli(), Form: "keep", Status: 200,
		})
	}
	date := time.Now().Format("20060102")
	rec := doAdminRequest(t, srv, http.MethodGet,
		"/direct-admin/api/system-prompt/events?date="+date+"&page=1&size=2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Events  []systemprompt.Event `json:"events"`
		Total   int                  `json:"total"`
		HasMore bool                 `json:"hasMore"`
		Page    int                  `json:"page"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Total != 5 || len(payload.Events) != 2 || !payload.HasMore || payload.Page != 1 {
		t.Fatalf("分页不对：%+v", payload)
	}
}

func TestSystemPromptEventsHugePageDoesNotPanic(t *testing.T) {
	srv := systemPromptTestServer(t)
	for i := 0; i < 3; i++ {
		srv.Svc.RecordSystemPromptEvent(systemprompt.Event{
			At: time.Now().UnixMilli(), Form: "keep", Status: 200,
		})
	}
	date := time.Now().Format("20060102")
	// (page-1)*size 会溢出成负数；旧代码以负下标切片 all[start:end] 直接 panic。
	rec := doAdminRequest(t, srv, http.MethodGet,
		"/direct-admin/api/system-prompt/events?date="+date+"&page=100000000000000000&size=100", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Events []systemprompt.Event `json:"events"`
		Total  int                  `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Events) != 0 {
		t.Fatalf("越界页 events 应为空，实际 %d 条", len(payload.Events))
	}
	if payload.Total != 3 {
		t.Fatalf("total 应仍为 3，实际 %d", payload.Total)
	}
}

func TestSystemPromptEventsOnlyProblems(t *testing.T) {
	srv := systemPromptTestServer(t)
	now := time.Now().UnixMilli()
	srv.Svc.RecordSystemPromptEvent(systemprompt.Event{At: now, Form: "keep", Status: 200})
	srv.Svc.RecordSystemPromptEvent(systemprompt.Event{At: now, Form: "fold", Status: 0})
	srv.Svc.RecordSystemPromptEvent(systemprompt.Event{At: now, Form: "keep", Status: 502, ErrorCode: "11128", Degraded: true})
	srv.Svc.RecordSystemPromptEvent(systemprompt.Event{At: now, Form: "fold", Status: 200})

	date := time.Now().Format("20060102")
	rec := doAdminRequest(t, srv, http.MethodGet,
		"/direct-admin/api/system-prompt/events?date="+date+"&onlyProblems=1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d", rec.Code)
	}
	var payload struct {
		Events []systemprompt.Event `json:"events"`
		Total  int                  `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	// 传输层失败（Status=0）必须算问题。
	if payload.Total != 2 || len(payload.Events) != 2 {
		t.Fatalf("只看问题应筛出 2 条，实际 total=%d len=%d：%+v", payload.Total, len(payload.Events), payload.Events)
	}
	for _, e := range payload.Events {
		if e.Status == 200 && !e.Degraded && e.ErrorCode == "" {
			t.Fatalf("正常事件不应出现在只看问题里：%+v", e)
		}
	}
}

func TestSystemPromptOverviewShape(t *testing.T) {
	srv := systemPromptTestServer(t)
	srv.Svc.RecordSystemPromptEvent(systemprompt.Event{
		At: time.Now().UnixMilli(), Form: "keep", Status: 200,
	})
	rec := doAdminRequest(t, srv, http.MethodGet, "/direct-admin/api/system-prompt", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d", rec.Code)
	}
	var payload struct {
		Mode   string               `json:"mode"`
		Stats  systemprompt.Stats   `json:"stats"`
		Recent []systemprompt.Event `json:"recent"`
		Dates  []string             `json:"dates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Stats.Total != 1 || len(payload.Recent) != 1 {
		t.Fatalf("概览不对：%+v", payload)
	}
	if len(payload.Dates) != 1 || payload.Dates[0] != time.Now().Format("20060102") {
		t.Fatalf("dates 不对：%v", payload.Dates)
	}
}

func TestStatusIncludesSystemPromptMode(t *testing.T) {
	srv := systemPromptTestServer(t)
	srv.Svc.SetBehaviorSwitches(switchesWith(srv, func(b *gateway.BehaviorSwitches) {
		b.SystemPromptMode = config.SystemPromptModeFold
	}))
	status := srv.Svc.Status()
	if got, _ := status["systemPromptMode"].(string); got != config.SystemPromptModeFold {
		t.Fatalf("status.systemPromptMode=%v want fold", status["systemPromptMode"])
	}
}

// switchesWith 以当前配置为基线构造一份完整开关集合，
// 避免 SetBehaviorSwitches 的整批赋值把别的开关清零。
func switchesWith(srv *Server, mutate func(*gateway.BehaviorSwitches)) gateway.BehaviorSwitches {
	cfg := srv.Svc.Config()
	next := gateway.BehaviorSwitches{
		CheckinAllowDirectFallback: cfg.CheckinAllowDirectFallback,
		QuotaFloorCredits:          cfg.QuotaFloorCredits,
		CheckinRetryInterval:       cfg.CheckinRetryInterval,
		CheckinRetryMax:            cfg.CheckinRetryMax,
		SystemPromptMode:           cfg.SystemPromptMode,
	}
	mutate(&next)
	return next
}

func TestRecordChatJournalCarriesSystemOutcome(t *testing.T) {
	srv := systemPromptTestServer(t)
	outcome := &gateway.SystemPromptOutcome{Form: "fold", Fingerprint: "abcdef12", Degraded: true}
	opts := gateway.CompleteOptions{
		Messages:      []map[string]any{{"role": "system", "content": "S"}},
		SystemOutcome: outcome,
	}
	srv.recordChatJournal(time.Now(), "req-sp-1", opts, "auto", false, true, "", provider.Usage{}, nil)

	view := srv.Svc.Journal.View("all", 10, 0)
	if len(view.Requests) != 1 {
		t.Fatalf("应写 1 条用量明细，实际 %d", len(view.Requests))
	}
	entry := view.Requests[0]
	if entry.SystemForm != "fold" || entry.SystemFp != "abcdef12" || !entry.SystemDegraded {
		t.Fatalf("用量明细未带上 system 形态：%+v", entry)
	}
}
