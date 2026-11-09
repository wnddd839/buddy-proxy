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

const retryTestInterval = 10 * time.Minute

// retryRunner 造一个带重试能力的 Runner，返回它、日志和「基准时刻」。
// 基准时刻取「今天全天窗口掷出的那一刻」，后续 tick 都基于它推进，
// 避免用 time.Now() 导致时间跨度不可控。
func retryRunner(t *testing.T, dir string, store accounts.Store, auto bool) (*Runner, *Log, time.Time) {
	t.Helper()
	pool := checkinTestPool(t, dir, store)
	file := Open(filepath.Join(dir, "proxy-checkin.json"))
	if err := file.Write(Store{Rules: []Rule{
		{AccountID: store.Accounts[0].ID, Enabled: true, Start: "08:00", End: ""},
	}}); err != nil {
		t.Fatal(err)
	}
	results := OpenLog(filepath.Join(dir, "proxy-checkin-log.json"))
	r := NewRunner(file, results, pool, provider.NewClient(config.Config{}), nil, func() config.Config {
		return config.Config{
			CheckinAuto:          auto,
			CheckinRetryInterval: retryTestInterval,
			CheckinRetryMax:      1,
		}
	})
	// 起点固定在「今天 08:00（北京）」，全天窗口一定包含它且远早于 23:30 截止。
	loc := time.FixedZone("UTC+8", 8*3600)
	now := time.Now().In(loc)
	base := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, loc)
	return r, results, base
}

// tickAt 在 base 时刻前后各 1 秒的窗口内触发一次 due()。
func tickAt(t *testing.T, r *Runner, base time.Time, offset time.Duration) {
	t.Helper()
	at := base.Add(offset)
	r.due(t.Context(), at.Add(-time.Second), at.Add(time.Second))
}

// 第一次失败后，过了重试间隔再 tick ⇒ 应该重试（第二次成功）。
func TestRetryAfterFailure(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":500}`))
			return
		}
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
	r, results, base := retryRunner(t, dir, store, true)
	day := base.Format("2006-01-02")

	tickAt(t, r, base, 0)
	if e, ok := results.Today("a1", day); !ok || e.OK {
		t.Fatalf("第一次应失败，got ok=%v %+v", ok, e)
	}

	// 推进到「首次 + 11 分钟」，再 tick ⇒ 应重试
	tickAt(t, r, base, 11*time.Minute)
	if !results.SucceededToday("a1", day) {
		t.Fatalf("过了重试间隔应重试成功，entries=%+v", mustRead(t, results))
	}
	if got := results.AttemptsToday("a1", day); got != 2 {
		t.Fatalf("应恰好尝试 2 次，got %d", got)
	}
}

// 重试间隔没到 ⇒ 不该重试。
func TestRetryRespectsInterval(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":500}`))
	}))
	defer upstream.Close()

	dir := t.TempDir()
	store := accounts.Store{Accounts: []accounts.Account{checkinTestAccount(upstream.URL, "a1")}}
	r, results, base := retryRunner(t, dir, store, true)
	day := base.Format("2006-01-02")

	tickAt(t, r, base, 0)
	first := calls.Load()
	if first == 0 {
		t.Fatal("首次应有请求")
	}

	// 只推进 1 分钟（< 10 分钟间隔）
	tickAt(t, r, base, 1*time.Minute)
	if calls.Load() != first {
		t.Fatalf("间隔未到不该重试，calls %d -> %d", first, calls.Load())
	}
	if got := results.AttemptsToday("a1", day); got != 1 {
		t.Fatalf("应仍只有 1 次尝试，got %d", got)
	}
}

// 已经成功 ⇒ 不再重试（即使有过失败记录）。
func TestNoRetryAfterSuccess(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
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
	r, results, base := retryRunner(t, dir, store, true)
	day := base.Format("2006-01-02")

	tickAt(t, r, base, 0)
	if !results.SucceededToday("a1", day) {
		t.Fatal("第一次应成功")
	}
	before := calls.Load()

	tickAt(t, r, base, 15*time.Minute)
	if calls.Load() != before {
		t.Fatalf("已成功不该再签到，calls %d -> %d", before, calls.Load())
	}
}

// 重试次数用完 ⇒ 不再重试。
func TestRetryBudgetExhausted(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":500}`))
	}))
	defer upstream.Close()

	dir := t.TempDir()
	store := accounts.Store{Accounts: []accounts.Account{checkinTestAccount(upstream.URL, "a1")}}
	r, results, base := retryRunner(t, dir, store, true) // CheckinRetryMax = 1
	day := base.Format("2006-01-02")

	tickAt(t, r, base, 0)              // 首次
	tickAt(t, r, base, 11*time.Minute) // 第一次重试
	tickAt(t, r, base, 30*time.Minute) // 预算已用完

	if got := results.AttemptsToday("a1", day); got != 2 {
		t.Fatalf("首次 + 1 次重试 = 2 条，got %d", got)
	}
}

// 关掉总开关时重试也不该跑。
func TestRetrySkippedWhenAutoOff(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	dir := t.TempDir()
	store := accounts.Store{Accounts: []accounts.Account{checkinTestAccount(upstream.URL, "a1")}}
	r, results, base := retryRunner(t, dir, store, false)
	day := base.Format("2006-01-02")

	tickAt(t, r, base, 0)
	tickAt(t, r, base, 20*time.Minute)

	if calls.Load() != 0 {
		t.Fatalf("总开关关闭时不该有任何请求，got %d", calls.Load())
	}
	if got := results.AttemptsToday("a1", day); got != 0 {
		t.Fatalf("不该有记录，got %d", got)
	}
}

// 超过 23:30 截止后不再重试。
func TestRetryStopsAfterCutoff(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":500}`))
	}))
	defer upstream.Close()

	dir := t.TempDir()
	store := accounts.Store{Accounts: []accounts.Account{checkinTestAccount(upstream.URL, "a1")}}
	r, results, base := retryRunner(t, dir, store, true)
	day := base.Format("2006-01-02")

	// 首次放到 23:00，重试窗口推到 23:40（已过 23:30 截止）
	loc := base.Location()
	late := base
	r.due(t.Context(), late.Add(-time.Second), late.Add(time.Second))
	first := results.AttemptsToday("a1", day)
	if first != 1 {
		t.Fatalf("expected an initial failed attempt, got %d", first)
	}

	after := time.Date(base.Year(), base.Month(), base.Day(), 23, 40, 0, 0, loc)
	r.due(t.Context(), after.Add(-time.Second), after.Add(time.Second))

	if results.AttemptsToday("a1", day) != first {
		t.Fatalf("23:30 后不该重试，attempts %d -> %d", first, results.AttemptsToday("a1", day))
	}
}

func mustRead(t *testing.T, l *Log) []Entry {
	t.Helper()
	es, err := l.Read()
	if err != nil {
		t.Fatal(err)
	}
	return es
}
