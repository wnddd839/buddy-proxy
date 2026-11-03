package concurrency

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBeginTracksInFlight(t *testing.T) {
	tr := New("", time.Hour)
	defer tr.Close()

	if got := tr.AccountInFlight("a"); got != 0 {
		t.Fatalf("初始 in-flight=%d want 0", got)
	}
	end1 := tr.Begin("a", "账号A")
	end2 := tr.Begin("a", "账号A")
	if got := tr.AccountInFlight("a"); got != 2 {
		t.Fatalf("两个在途 in-flight=%d want 2", got)
	}
	if got := tr.GlobalInFlight(); got != 2 {
		t.Fatalf("全局 in-flight=%d want 2", got)
	}
	end1(true)
	if got := tr.AccountInFlight("a"); got != 1 {
		t.Fatalf("结束一个后 in-flight=%d want 1", got)
	}
	if got := tr.GlobalInFlight(); got != 1 {
		t.Fatalf("全局 in-flight=%d want 1", got)
	}
	end2(false)
	if got := tr.AccountInFlight("a"); got != 0 {
		t.Fatalf("全部结束 in-flight=%d want 0", got)
	}
	if got := tr.GlobalInFlight(); got != 0 {
		t.Fatalf("全局 in-flight=%d want 0", got)
	}
}

func TestEndIsIdempotent(t *testing.T) {
	tr := New("", time.Hour)
	defer tr.Close()

	end := tr.Begin("a", "")
	end(true)
	end(true) // 重复调用不得把计数打成负数
	if got := tr.AccountInFlight("a"); got != 0 {
		t.Fatalf("in-flight=%d want 0", got)
	}
	if got := tr.GlobalInFlight(); got != 0 {
		t.Fatalf("global=%d want 0", got)
	}
}

func TestSnapshotCountsRequestsAndFailures(t *testing.T) {
	tr := New("", time.Hour)
	defer tr.Close()

	tr.Begin("a", "账号A")(true)
	tr.Begin("a", "账号A")(true)
	tr.Begin("a", "账号A")(false)

	snap := tr.Snapshot()
	if len(snap.Accounts) != 1 {
		t.Fatalf("accounts=%d want 1", len(snap.Accounts))
	}
	v := snap.Accounts[0]
	if v.AccountID != "a" || v.Label != "账号A" {
		t.Fatalf("account=%+v", v)
	}
	if v.Requests60 != 3 {
		t.Fatalf("requests60=%d want 3", v.Requests60)
	}
	if v.Failures60 != 1 {
		t.Fatalf("failures60=%d want 1", v.Failures60)
	}
	if v.RPM1 != 3 || v.RPM5 != 3 || v.RPM60 != 3 {
		t.Fatalf("rpm1=%d rpm5=%d rpm60=%d want 3/3/3", v.RPM1, v.RPM5, v.RPM60)
	}
	if snap.Totals.Requests != 3 || snap.Totals.Failures != 1 {
		t.Fatalf("totals=%+v", snap.Totals)
	}
}

func TestPeakInFlightCaptured(t *testing.T) {
	tr := New("", time.Hour)
	defer tr.Close()

	e1 := tr.Begin("a", "")
	e2 := tr.Begin("a", "")
	e3 := tr.Begin("a", "")
	e1(true)
	e2(true)
	e3(true)

	v := tr.Snapshot().Accounts[0]
	if v.PeakInFlight != 3 {
		t.Fatalf("peak=%d want 3", v.PeakInFlight)
	}
}

func TestRecordRateLimitCapturesInFlight(t *testing.T) {
	tr := New("", time.Hour)
	defer tr.Close()

	// 先造 4 个在途
	ends := make([]func(bool), 0, 4)
	for i := 0; i < 4; i++ {
		ends = append(ends, tr.Begin("a", "账号A"))
	}
	// 429 在第 5 个请求上触发（模拟：先 Begin 再 Record）
	ends = append(ends, tr.Begin("a", "账号A"))
	tr.RecordRateLimit("a", "账号A", "deepseek-v4.1-flash", "domestic", "429 too many", 0)

	snap := tr.Snapshot()
	if len(snap.RateLimits) != 1 {
		t.Fatalf("rateLimits=%d want 1", len(snap.RateLimits))
	}
	ev := snap.RateLimits[0]
	if ev.AccountInFlight != 5 {
		t.Fatalf("accountInFlight=%d want 5", ev.AccountInFlight)
	}
	if ev.GlobalInFlight != 5 {
		t.Fatalf("globalInFlight=%d want 5", ev.GlobalInFlight)
	}
	v := snap.Accounts[0]
	if v.RateLimitInFlightMin != 5 || v.RateLimitInFlightMax != 5 {
		t.Fatalf("min/max=%d/%d want 5/5", v.RateLimitInFlightMin, v.RateLimitInFlightMax)
	}
	if v.SafeConcurrency != 4 {
		t.Fatalf("safeConcurrency=%d want 4", v.SafeConcurrency)
	}
	if v.RateLimits60 != 1 {
		t.Fatalf("rateLimits60=%d want 1", v.RateLimits60)
	}
	for _, end := range ends {
		end(true)
	}
}

func TestRateLimitKindClassification(t *testing.T) {
	tr := New("", time.Hour)
	defer tr.Close()

	now := time.Now()
	// 30 分钟后重置 => 频率限制
	tr.RecordRateLimit("a", "", "", "", "rate", now.Add(30*time.Minute).UnixMilli())
	// 20 小时后重置 => 日配额
	tr.RecordRateLimit("b", "", "", "", "quota", now.Add(20*time.Hour).UnixMilli())

	snap := tr.Snapshot()
	byAccount := map[string]RateLimitEvent{}
	for _, ev := range snap.RateLimits {
		byAccount[ev.AccountID] = ev
	}
	if got := byAccount["a"].Kind; got != "rate" {
		t.Fatalf("30min kind=%q want rate", got)
	}
	if got := byAccount["b"].Kind; got != "quota" {
		t.Fatalf("20h kind=%q want quota", got)
	}
}

func TestSafeConcurrencyUsesMinObserved(t *testing.T) {
	tr := New("", time.Hour)
	defer tr.Close()

	// 先在 3 并发时限流，再在 8 并发时限流 => 安全上界取最小的那个-1
	ends := make([]func(bool), 0)
	for i := 0; i < 3; i++ {
		ends = append(ends, tr.Begin("a", ""))
	}
	tr.RecordRateLimit("a", "", "", "", "first", 0)
	for _, e := range ends {
		e(true)
	}
	for i := 0; i < 8; i++ {
		ends = append(ends, tr.Begin("a", ""))
	}
	tr.RecordRateLimit("a", "", "", "", "second", 0)
	for _, e := range ends[3:] {
		e(true)
	}

	v := tr.Snapshot().Accounts[0]
	if v.RateLimitInFlightMin != 3 {
		t.Fatalf("min=%d want 3", v.RateLimitInFlightMin)
	}
	if v.RateLimitInFlightMax != 8 {
		t.Fatalf("max=%d want 8", v.RateLimitInFlightMax)
	}
	if v.SafeConcurrency != 2 {
		t.Fatalf("safe=%d want 2", v.SafeConcurrency)
	}
}

func TestEventsPersistAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy-concurrency.json")

	tr := New(path, time.Hour)
	tr.RecordRateLimit("a", "账号A", "m1", "domestic", "429", 0)
	if err := tr.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var snap diskSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(snap.Events) != 1 || snap.Events[0].AccountID != "a" {
		t.Fatalf("persisted events=%+v", snap.Events)
	}

	// 重新打开应能读回
	tr2 := New(path, time.Hour)
	defer tr2.Close()
	got := tr2.Snapshot()
	if len(got.RateLimits) != 1 {
		t.Fatalf("reloaded rateLimits=%d want 1", len(got.RateLimits))
	}
	if got.RateLimits[0].Label != "账号A" {
		t.Fatalf("reloaded label=%q", got.RateLimits[0].Label)
	}
}

func TestExpiredEventsDroppedOnLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy-concurrency.json")

	old := diskSnapshot{
		Version: 1,
		Events: []RateLimitEvent{
			{At: time.Now().Add(-48 * time.Hour).UnixMilli(), AccountID: "old"},
			{At: time.Now().Add(-1 * time.Minute).UnixMilli(), AccountID: "fresh"},
		},
	}
	raw, _ := json.Marshal(old)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	tr := New(path, time.Hour)
	defer tr.Close()
	got := tr.Snapshot()
	if len(got.RateLimits) != 1 {
		t.Fatalf("rateLimits=%d want 1（过期事件应被丢弃）", len(got.RateLimits))
	}
	if got.RateLimits[0].AccountID != "fresh" {
		t.Fatalf("kept=%q want fresh", got.RateLimits[0].AccountID)
	}
}

func TestNilTrackerIsSafe(t *testing.T) {
	var tr *Tracker
	end := tr.Begin("a", "")
	end(true)
	tr.RecordRateLimit("a", "", "", "", "", 0)
	if got := tr.GlobalInFlight(); got != 0 {
		t.Fatalf("nil global=%d", got)
	}
	if got := tr.AccountInFlight("a"); got != 0 {
		t.Fatalf("nil account=%d", got)
	}
	if got := tr.Snapshot(); got.Accounts != nil {
		t.Fatalf("nil snapshot=%+v", got)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("nil close: %v", err)
	}
}

func TestConcurrentBeginEnd(t *testing.T) {
	tr := New("", time.Hour)
	defer tr.Close()

	const workers = 32
	const perWorker = 50
	done := make(chan struct{}, workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < perWorker; j++ {
				end := tr.Begin("a", "")
				end(j%3 != 0)
			}
		}()
	}
	for i := 0; i < workers; i++ {
		<-done
	}

	if got := tr.GlobalInFlight(); got != 0 {
		t.Fatalf("global in-flight=%d want 0", got)
	}
	v := tr.Snapshot().Accounts[0]
	if v.Requests60 != workers*perWorker {
		t.Fatalf("requests60=%d want %d", v.Requests60, workers*perWorker)
	}
}

func TestLabelBackfilledOnRateLimit(t *testing.T) {
	tr := New("", time.Hour)
	defer tr.Close()

	end := tr.Begin("a", "我的账号")
	tr.RecordRateLimit("a", "", "", "", "429", 0) // 不传 label，应从 Begin 回填
	end(true)

	ev := tr.Snapshot().RateLimits[0]
	if ev.Label != "我的账号" {
		t.Fatalf("label=%q want 我的账号", ev.Label)
	}
}

// TestSafeConcurrencyIgnoresZeroInFlight 固定住 2026-11-02 实测发现的缺陷：
// 429 常有一批是「并发已经散开之后」才收到响应（此时 inFlight=0），
// 若把这类样本算进最小值，safeConcurrency 会永远被压成 0，指标直接失效。
func TestSafeConcurrencyIgnoresZeroInFlight(t *testing.T) {
	tr := New("", time.Hour)
	defer tr.Close()

	tr.RecordRateLimit("a", "", "", "", "散开后的 429", 0)
	tr.RecordRateLimit("a", "", "", "", "散开后的 429", 0)

	ends := make([]func(bool), 0, 6)
	for i := 0; i < 6; i++ {
		ends = append(ends, tr.Begin("a", ""))
	}
	tr.RecordRateLimit("a", "", "", "", "并发打满的 429", 0)
	for _, e := range ends {
		e(true)
	}

	v := tr.Snapshot().Accounts[0]
	if v.RateLimitInFlightMin != 6 {
		t.Fatalf("min=%d want 6（应忽略 inFlight=0 的样本）", v.RateLimitInFlightMin)
	}
	if v.SafeConcurrency != 5 {
		t.Fatalf("safe=%d want 5", v.SafeConcurrency)
	}
}

// TestSafeConcurrencyZeroWhenOnlySingleFlight 单请求就被限流时不能推出安全上界。
func TestSafeConcurrencyZeroWhenOnlySingleFlight(t *testing.T) {
	tr := New("", time.Hour)
	defer tr.Close()

	end := tr.Begin("a", "")
	tr.RecordRateLimit("a", "", "", "", "单并发就限流", 0)
	end(true)

	v := tr.Snapshot().Accounts[0]
	if v.RateLimitInFlightMin != 1 {
		t.Fatalf("min=%d want 1", v.RateLimitInFlightMin)
	}
	if v.SafeConcurrency != 0 {
		t.Fatalf("safe=%d want 0（单并发即限流，推不出上界）", v.SafeConcurrency)
	}
}
