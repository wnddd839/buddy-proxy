package checkin

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

func nextRunRunner(t *testing.T, dir string, rules []Rule) *Runner {
	t.Helper()
	f := Open(filepath.Join(dir, "r.json"))
	if err := f.Write(Store{Rules: rules}); err != nil {
		t.Fatal(err)
	}
	return NewRunner(f, OpenLog(filepath.Join(dir, "log.json")), nil, nil, nil,
		func() config.Config { return config.Config{} })
}

// NextRun 今天还没到点 ⇒ 返回今天的时刻。
func TestNextRunToday(t *testing.T) {
	dir := t.TempDir()
	r := nextRunRunner(t, dir, []Rule{{AccountID: "a1", Enabled: true, Start: "23:00"}})

	loc := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, 11, 7, 9, 0, 0, 0, loc)

	at, ok := r.NextRun("a1", now)
	if !ok {
		t.Fatal("应有下次运行时间")
	}
	if at.In(loc).Hour() != 23 || at.In(loc).Minute() != 0 {
		t.Fatalf("want 23:00, got %s", at.In(loc).Format("15:04"))
	}
	if at.In(loc).Day() != 7 {
		t.Fatalf("want 今天(7日), got %d日", at.In(loc).Day())
	}
}

// 今天的已经过了 ⇒ 返回明天的同一时刻。
func TestNextRunRollsToTomorrow(t *testing.T) {
	dir := t.TempDir()
	r := nextRunRunner(t, dir, []Rule{{AccountID: "a1", Enabled: true, Start: "08:00"}})

	loc := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, 11, 7, 9, 0, 0, 0, loc) // 09:00，今天的 08:00 已过

	at, ok := r.NextRun("a1", now)
	if !ok {
		t.Fatal("应有下次运行时间")
	}
	if at.In(loc).Day() != 8 {
		t.Fatalf("want 明天(8日), got %d日", at.In(loc).Day())
	}
	if at.In(loc).Hour() != 8 {
		t.Fatalf("want 08:00, got %s", at.In(loc).Format("15:04"))
	}
}

// 全天随机：同一天多次调用必须得到同一时刻（跨重启稳定）。
func TestNextRunAllDayIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	r := nextRunRunner(t, dir, []Rule{{AccountID: "a1", Enabled: true}})

	loc := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, 11, 7, 0, 0, 1, 0, loc)

	first, ok := r.NextRun("a1", now)
	if !ok {
		t.Fatal("应有下次运行时间")
	}
	for i := 0; i < 5; i++ {
		again, _ := r.NextRun("a1", now)
		if !again.Equal(first) {
			t.Fatalf("同一天必须稳定，%s != %s", again, first)
		}
	}
	if first.In(loc).Day() != 7 {
		t.Fatalf("want 今天, got %d日", first.In(loc).Day())
	}
}

// 未配置规则的账号 ⇒ ok=false。
func TestNextRunNoRule(t *testing.T) {
	dir := t.TempDir()
	r := nextRunRunner(t, dir, nil)
	if _, ok := r.NextRun("ghost", time.Now()); ok {
		t.Fatal("无规则时不该返回时间")
	}
}

// 规则存在但 enabled=false ⇒ 不算启用，ok=false。
func TestNextRunDisabledRule(t *testing.T) {
	dir := t.TempDir()
	r := nextRunRunner(t, dir, []Rule{{AccountID: "a1", Enabled: false, Start: "09:00"}})
	if _, ok := r.NextRun("a1", time.Now()); ok {
		t.Fatal("未启用的规则不该返回时间")
	}
}

// 时段窗口：返回的时刻必须落在窗口内。
func TestNextRunWithinWindow(t *testing.T) {
	dir := t.TempDir()
	r := nextRunRunner(t, dir, []Rule{{AccountID: "a1", Enabled: true, Start: "09:00", End: "11:00"}})

	loc := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, 11, 7, 0, 0, 30, 0, loc)

	at, ok := r.NextRun("a1", now)
	if !ok {
		t.Fatal("应有下次运行时间")
	}
	h, m := at.In(loc).Hour(), at.In(loc).Minute()
	if h < 9 || h > 11 || (h == 11 && m > 0) {
		t.Fatalf("时刻 %s 落在 09:00-11:00 窗口外", at.In(loc).Format("15:04"))
	}
}
