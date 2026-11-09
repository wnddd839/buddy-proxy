package checkin_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/checkin"
)

func TestLogAppendAndRead(t *testing.T) {
	dir := t.TempDir()
	l := checkin.OpenLog(filepath.Join(dir, "proxy-checkin-log.json"))

	if err := l.Append(checkin.Entry{
		AccountID: "a1", Day: "2026-11-06", At: time.Now().UnixMilli(),
		OK: true, Credits: 100,
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	entries, err := l.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 1 || entries[0].AccountID != "a1" || !entries[0].OK {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestLogPrunesOlderThan48h(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy-checkin-log.json")
	l := checkin.OpenLog(path)

	now := time.Now()
	old := now.Add(-49 * time.Hour).UnixMilli()
	keep := now.Add(-47 * time.Hour).UnixMilli()

	if err := l.Append(checkin.Entry{AccountID: "old", At: old, Day: "2026-11-04"}); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(checkin.Entry{AccountID: "keep", At: keep, Day: "2026-11-05"}); err != nil {
		t.Fatal(err)
	}

	entries, err := l.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].AccountID != "keep" {
		t.Fatalf("expected only 'keep', got %+v", entries)
	}
}

func TestLogKeepsBoundaryAtExactly48h(t *testing.T) {
	dir := t.TempDir()
	l := checkin.OpenLog(filepath.Join(dir, "log.json"))
	now := time.Now()
	boundary := now.Add(-checkin.Retention).UnixMilli()
	if err := l.Append(checkin.Entry{AccountID: "edge", At: boundary}); err != nil {
		t.Fatal(err)
	}
	entries, _ := l.Read()
	if len(entries) != 1 {
		t.Fatalf("boundary entry must be kept, got %d", len(entries))
	}
}

func TestLogCount48h(t *testing.T) {
	dir := t.TempDir()
	l := checkin.OpenLog(filepath.Join(dir, "log.json"))
	now := time.Now()
	_ = l.Append(checkin.Entry{AccountID: "x", At: now.Add(-1 * time.Hour).UnixMilli(), OK: true})
	_ = l.Append(checkin.Entry{AccountID: "x", At: now.Add(-25 * time.Hour).UnixMilli(), OK: true})
	_ = l.Append(checkin.Entry{AccountID: "x", At: now.Add(-40 * time.Hour).UnixMilli(), OK: true})
	_ = l.Append(checkin.Entry{AccountID: "x", At: now.Add(-60 * time.Hour).UnixMilli(), OK: true})
	_ = l.Append(checkin.Entry{AccountID: "y", At: now.Add(-1 * time.Hour).UnixMilli(), OK: true})
	_ = l.Append(checkin.Entry{AccountID: "x", At: now.Add(-2 * time.Hour).UnixMilli(), OK: false})

	if got := l.Count48h("x", now); got != 3 {
		t.Fatalf("Count48h: want 3 (只在 48h 内且 OK), got %d", got)
	}
	if got := l.Count48h("y", now); got != 1 {
		t.Fatalf("Count48h(y): want 1, got %d", got)
	}
	if got := l.Count48h("none", now); got != 0 {
		t.Fatalf("Count48h(none): want 0, got %d", got)
	}
}

func TestLogTodayReturnsLastEntry(t *testing.T) {
	dir := t.TempDir()
	l := checkin.OpenLog(filepath.Join(dir, "log.json"))
	now := time.Now()
	_ = l.Append(checkin.Entry{AccountID: "x", Day: "2026-11-06", At: now.Add(-2 * time.Hour).UnixMilli(), OK: false})
	_ = l.Append(checkin.Entry{AccountID: "x", Day: "2026-11-06", At: now.Add(-1 * time.Hour).UnixMilli(), OK: true})
	_ = l.Append(checkin.Entry{AccountID: "x", Day: "2026-11-05", At: now.Add(-30 * time.Hour).UnixMilli(), OK: true})

	e, ok := l.Today("x", "2026-11-06")
	if !ok || !e.OK {
		t.Fatalf("Today should return the last entry, got ok=%v entry=%+v", ok, e)
	}
	if _, ok := l.Today("x", "2026-11-07"); ok {
		t.Fatal("Today 对不存在的日期应返回 false")
	}
}

func TestLogFileMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not describe Windows ACLs")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "log.json")
	l := checkin.OpenLog(path)
	if err := l.Append(checkin.Entry{AccountID: "x", At: time.Now().UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("want 0600, got %o", perm)
	}
}

func TestLogConcurrentAppendKeepsFileValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.json")
	l := checkin.OpenLog(path)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = l.Append(checkin.Entry{AccountID: "c", At: time.Now().UnixMilli(), OK: true})
		}(i)
	}
	wg.Wait()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("并发写入后文件必须是完整 JSON: %v", err)
	}
	entries, err := l.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 20 {
		t.Fatalf("20 次并发 append 应全部落盘，got %d", len(entries))
	}
}

func TestLogJSONShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.json")
	l := checkin.OpenLog(path)
	_ = l.Append(checkin.Entry{
		AccountID: "a1", Day: "2026-11-06", At: 1793952000000,
		OK: true, Already: false, Credits: 100, Message: "", Egress: "socks5h://127.0.0.1:18143", Degraded: false,
	})
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if _, ok := doc["version"]; !ok {
		t.Fatal("missing version field")
	}
	if _, ok := doc["entries"]; !ok {
		t.Fatal("missing entries field")
	}
}

func TestLogCapsAt200Entries(t *testing.T) {
	dir := t.TempDir()
	l := checkin.OpenLog(filepath.Join(dir, "log.json"))
	now := time.Now()
	for i := 0; i < 210; i++ {
		if err := l.Append(checkin.Entry{
			AccountID: "cap", At: now.Add(-time.Duration(i) * time.Minute).UnixMilli(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := l.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > 200 {
		t.Fatalf("应最多保留 200 条，实际 %d", len(entries))
	}
}

func TestLogReadMissingFile(t *testing.T) {
	l := checkin.OpenLog(filepath.Join(t.TempDir(), "nope.json"))
	entries, err := l.Read()
	if err != nil {
		t.Fatalf("文件不存在应返回空切片与 nil，got err=%v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("want empty, got %+v", entries)
	}
	if got := l.Count48h("x", time.Now()); got != 0 {
		t.Fatalf("want 0, got %d", got)
	}
}
