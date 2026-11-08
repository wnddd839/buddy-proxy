package checkin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

// checkinTestRunner 造一个能跑 due() 的 Runner，并返回它用的 log。
func checkinTestRunner(t *testing.T, dir string, store accounts.Store, allowFallback bool) (*Runner, *Log) {
	t.Helper()
	pool := checkinTestPool(t, dir, store)
	file := Open(filepath.Join(dir, "proxy-checkin.json"))
	if err := file.Write(Store{Rules: []Rule{
		{AccountID: store.Accounts[0].ID, Enabled: true, Start: "", End: ""},
	}}); err != nil {
		t.Fatal(err)
	}
	results := OpenLog(filepath.Join(dir, "proxy-checkin-log.json"))
	runner := NewRunner(file, results, pool, provider.NewClient(config.Config{}), nil, func() config.Config {
		return config.Config{CheckinAuto: true, CheckinAllowDirectFallback: allowFallback}
	})
	return runner, results
}

// runDueNow 把 tick 区间对准「今天全天窗口里掷出的那一刻」。
//
// 必须显式写 Mode: "allDay"：留空会走 window 分支（窗口内三角分布），
// 而规则文件经 normalizeRules 后 allDay 与 window 用的是不同算法，
// 这里若留空就与实际调度对不上，测试会随机失败。
func runDueNow(t *testing.T, runner *Runner, accountID string) {
	t.Helper()
	day := time.Now().In(beijing).Format("2006-01-02")
	// StartMin/EndMin 必须与 normalizeRules 推导出的一致（0 / 23:59），
	// 否则 dueAt 的 seed 不同、算出的时刻与实际调度对不上。
	at := runner.dueAt(normalizedRule{AccountID: accountID, Mode: "allDay", StartMin: 0, EndMin: 23*60 + 59}, day)
	runner.due(t.Context(), at.Add(-time.Second), at.Add(time.Second))
}

// 签到失败也要落一条 Entry，OK=false 且带 message。
func TestRunnerWritesEntryOnFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":500,"msg":"boom"}`))
	}))
	defer upstream.Close()

	dir := t.TempDir()
	store := accounts.Store{Accounts: []accounts.Account{checkinTestAccount(upstream.URL, "a1")}}
	runner, results := checkinTestRunner(t, dir, store, false)
	runDueNow(t, runner, "a1")

	day := time.Now().In(beijing).Format("2006-01-02")
	entry, ok := results.Today("a1", day)
	if !ok {
		t.Fatal("失败也必须落一条记录，否则前端徽章永远显示「未签到」")
	}
	if entry.OK {
		t.Fatal("上游 500 时 OK 应为 false")
	}
	if entry.Degraded {
		t.Fatal("未开启降级开关时 Degraded 应为 false")
	}
}

// 成功时落 OK=true 且 Credits 透传。
func TestRunnerWritesEntryOnSuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/billing/meter/checkin-activity-status":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"active": true, "today_checked_in": false}})
		case "/v2/billing/meter/daily-checkin":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"credit": 100}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	dir := t.TempDir()
	store := accounts.Store{Accounts: []accounts.Account{checkinTestAccount(upstream.URL, "a1")}}
	runner, results := checkinTestRunner(t, dir, store, false)
	runDueNow(t, runner, "a1")

	day := time.Now().In(beijing).Format("2006-01-02")
	entry, ok := results.Today("a1", day)
	if !ok || !entry.OK {
		t.Fatalf("成功应落 OK=true，got ok=%v entry=%+v", ok, entry)
	}
	if entry.Credits != 100 {
		t.Fatalf("Credits 应透传 100，got %d", entry.Credits)
	}
	if got := results.Count48h("a1", time.Now()); got != 1 {
		t.Fatalf("Count48h 应为 1，got %d", got)
	}
}

// 429 不属于出口错误，即使开了降级也不该重试，只落一条 degraded=false。
func TestRunnerDegradesOnlyOnEgressError(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":429,"msg":"rate limit"}`))
	}))
	defer upstream.Close()

	dir := t.TempDir()
	acct := checkinTestAccount(upstream.URL, "a1")
	store := accounts.Store{Accounts: []accounts.Account{acct}}
	runner, results := checkinTestRunner(t, dir, store, true)
	runDueNow(t, runner, "a1")

	day := time.Now().In(beijing).Format("2006-01-02")
	entry, ok := results.Today("a1", day)
	if !ok {
		t.Fatal("应落一条记录")
	}
	if entry.Degraded {
		t.Fatal("429 不是出口错误，不应标记 Degraded")
	}
	// 429 只应打一次签到动作 + 一次状态查询；不该出现第二轮
	if n := calls.Load(); n > 2 {
		t.Fatalf("429 不应触发降级重试，实际请求 %d 次", n)
	}
}

// 账号绑定的出口不可达 + 直连可达 ⇒ 降级成功，Degraded=true 且 OK=true。
func TestRunnerDegradeSucceedsOverDirect(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/billing/meter/checkin-activity-status":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"active": true, "today_checked_in": false}})
		case "/v2/billing/meter/daily-checkin":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"credit": 50}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	dir := t.TempDir()
	acct := checkinTestAccount(upstream.URL, "a1")
	// 指向一个必然连不上的 socks 出口
	acct.Proxy = "socks5h://127.0.0.1:1"
	store := accounts.Store{Accounts: []accounts.Account{acct}}
	runner, results := checkinTestRunner(t, dir, store, true)
	runDueNow(t, runner, "a1")

	day := time.Now().In(beijing).Format("2006-01-02")
	entry, ok := results.Today("a1", day)
	if !ok {
		t.Fatal("应落一条记录")
	}
	if !entry.Degraded {
		t.Fatalf("出口失败后回落直连成功，应标记 Degraded=true，got %+v", entry)
	}
	if !entry.OK {
		t.Fatalf("直连重试应成功，got entry=%+v", entry)
	}
}

// 降级开关关闭时，出口失败不重试，直接落 degraded=false 的失败记录。
func TestRunnerNoDegradeWhenSwitchOff(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"active": true, "today_checked_in": false}})
	}))
	defer upstream.Close()

	dir := t.TempDir()
	acct := checkinTestAccount(upstream.URL, "a1")
	acct.Proxy = "socks5h://127.0.0.1:1"
	store := accounts.Store{Accounts: []accounts.Account{acct}}
	runner, results := checkinTestRunner(t, dir, store, false)
	runDueNow(t, runner, "a1")

	day := time.Now().In(beijing).Format("2006-01-02")
	entry, ok := results.Today("a1", day)
	if !ok {
		t.Fatal("应落一条记录")
	}
	if entry.Degraded {
		t.Fatal("开关关闭时不应降级")
	}
	if entry.OK {
		t.Fatal("出口不可达且不降级，应为失败")
	}
}

// isEgressError 的判定边界。
func TestIsEgressError(t *testing.T) {
	yes := []string{
		"egress: socks CONNECT 响应失败",
		"dial tcp: i/o timeout",
		"connection refused",
		"connection reset by peer",
		"socks connect failed",
		"no route to host",
		"network is unreachable",
	}
	for _, msg := range yes {
		if !isEgressError(msg) {
			t.Errorf("应判为出口错误: %q", msg)
		}
	}
	no := []string{
		"429 rate limit",
		"code 6004 使用量已超出频率限制",
		"code 11140 request illegal",
		"upstream error: model provider is temporarily unavailable",
		"",
	}
	for _, msg := range no {
		if isEgressError(msg) {
			t.Errorf("不应判为出口错误: %q", msg)
		}
	}
}
