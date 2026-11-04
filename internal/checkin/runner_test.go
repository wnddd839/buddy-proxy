package checkin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

// 端对端：假上游 + 真实号池 + 配置保存 + 调度触发，验证到点逐账号签到。
func TestRunnerChecksInAtPerAccountWindow(t *testing.T) {
	var statusCalls, actionCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/billing/meter/checkin-activity-status":
			statusCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"active": true, "today_checked_in": false}})
		case "/v2/billing/meter/daily-checkin":
			actionCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"credit": 100}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	dir := t.TempDir()
	t.Setenv("CODEBUDDY_PROXY_ENV_FILE", filepath.Join(dir, "proxy.env"))
	store := accounts.Store{Accounts: []accounts.Account{checkinTestAccount(upstream.URL, "a1")}}
	pool := checkinTestPool(t, dir, store)
	file := Open(filepath.Join(dir, "proxy-checkin.json"))
	if err := file.Write(Store{Rules: []Rule{{AccountID: "a1", Enabled: true, Start: "09:00", End: "11:00"}}}); err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(file, pool, provider.NewClient(config.Config{}), nil, func() config.Config {
		return config.Config{CheckinAuto: true}
	})
	if !runner.Auto() {
		t.Fatal("mock 配置开启时调度器应读到开启")
	}
	if err := runner.SetAuto(true); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "proxy.env")); err != nil || !strings.Contains(string(raw), "CODEBUDDY_PROXY_CHECKIN_AUTO=true") {
		t.Fatalf("总开关未写入 .env: raw=%q err=%v", raw, err)
	}

	// 取该账号当天在北京时间窗口内掷出的时刻，构造一次正好跨过它的 tick。
	rule := normalizedRule{AccountID: "a1", StartMin: 9 * 60, EndMin: 11 * 60}
	day := time.Now().In(beijing).Format("2006-01-02")
	at := dueAt(rule, day)
	if at.Before(beijing9AM(day)) || at.After(beijing9AM(day).Add(2*time.Hour)) {
		t.Fatalf("随机时刻落在窗口外: %s", at)
	}
	runner.due(t.Context(), at.Add(-time.Second), at.Add(time.Second))
	if statusCalls.Load() != 1 || actionCalls.Load() != 1 {
		t.Fatalf("到点未签到: status=%d action=%d", statusCalls.Load(), actionCalls.Load())
	}

	// 同一天重复 tick 不重跑；启动不补签（区间在时刻之前也完全不跑）。
	runner.due(t.Context(), at.Add(time.Second), at.Add(21*time.Second))
	runner.due(t.Context(), at.Add(-time.Hour), at.Add(-time.Minute))
	if statusCalls.Load() != 1 || actionCalls.Load() != 1 {
		t.Fatalf("重复/补签触发了多余请求: status=%d action=%d", statusCalls.Load(), actionCalls.Load())
	}
	if err := runner.SetAuto(false); err != nil {
		t.Fatal(err)
	}
	runner.due(t.Context(), at.Add(-time.Second), at.Add(time.Second))
	if statusCalls.Load() != 1 || actionCalls.Load() != 1 {
		t.Fatalf("关闭后仍在跑: status=%d action=%d", statusCalls.Load(), actionCalls.Load())
	}
}

func TestRulesRejectInvalidAndPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	file := Open(filepath.Join(dir, "proxy-checkin.json"))
	invalid := [][]Rule{
		{{AccountID: "", Enabled: true, Start: "09:00"}},
		{{AccountID: "a1", Enabled: true, Start: "24:00"}},
		{{AccountID: "a1", Enabled: true, Start: "09:00", End: "08:00"}},
		{{AccountID: "a1", Enabled: true, Start: "09:00"}, {AccountID: "a1", Enabled: true, Start: "10:00"}},
	}
	for i, rules := range invalid {
		if err := file.Write(Store{Rules: rules}); err == nil {
			t.Fatalf("case %d 应被拒绝", i)
		}
	}
	want := []Rule{{AccountID: "a1", Enabled: true, Start: "09:00", End: "11:00"}, {AccountID: "a2", Enabled: false, Start: "23:30"}}
	if err := file.Write(Store{Rules: want}); err != nil {
		t.Fatal(err)
	}
	got, err := file.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rules) != 2 || got.Rules[1].Enabled || got.Rules[0].End != "11:00" {
		t.Fatalf("roundtrip=%+v", got.Rules)
	}
	enabled, err := normalizeRules(got.Rules)
	if err != nil || len(enabled) != 1 || enabled[0].AccountID != "a1" {
		t.Fatalf("enabled=%+v err=%v", enabled, err)
	}
}

func beijing9AM(day string) time.Time {
	at, _ := time.ParseInLocation("2006-01-02", day, beijing)
	return at.Add(9 * time.Hour)
}

func checkinTestPool(t *testing.T, dir string, store accounts.Store) *accounts.Pool {
	t.Helper()
	path := filepath.Join(dir, "proxy-accounts.json")
	raw, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return accounts.NewPool(path)
}

func checkinTestAccount(baseURL, id string) accounts.Account {
	return accounts.Account{
		ID:          id,
		Site:        "global",
		Enabled:     true,
		BearerToken: "test-token",
		APIEndpoint: strings.TrimRight(baseURL, "/") + "/v2/chat/completions",
		AuthStatus:  accounts.AuthStatus{UserID: "user-1"},
	}
}
