package checkin_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/checkin"
)

func TestLogAttemptsToday(t *testing.T) {
	dir := t.TempDir()
	l := checkin.OpenLog(filepath.Join(dir, "log.json"))
	now := time.Now()
	day := "2026-11-07"

	// a1 今天两次尝试（一失败一成功）
	_ = l.Append(checkin.Entry{AccountID: "a1", Day: day, At: now.Add(-2 * time.Hour).UnixMilli(), OK: false})
	_ = l.Append(checkin.Entry{AccountID: "a1", Day: day, At: now.Add(-1 * time.Hour).UnixMilli(), OK: true})
	// a1 昨天一次（不该计入今天）
	_ = l.Append(checkin.Entry{AccountID: "a1", Day: "2026-11-06", At: now.Add(-25 * time.Hour).UnixMilli(), OK: true})

	if got := l.AttemptsToday("a1", day); got != 2 {
		t.Fatalf("AttemptsToday want 2, got %d", got)
	}
	if got := l.AttemptsToday("a2", day); got != 0 {
		t.Fatalf("无记录账号 want 0, got %d", got)
	}
}

func TestLogSucceededToday(t *testing.T) {
	dir := t.TempDir()
	l := checkin.OpenLog(filepath.Join(dir, "log.json"))
	now := time.Now()
	day := "2026-11-07"

	_ = l.Append(checkin.Entry{AccountID: "fail", Day: day, At: now.Add(-1 * time.Hour).UnixMilli(), OK: false})
	_ = l.Append(checkin.Entry{AccountID: "ok", Day: day, At: now.Add(-1 * time.Hour).UnixMilli(), OK: true})

	if l.SucceededToday("fail", day) {
		t.Fatal("只有失败记录时不该算成功")
	}
	if !l.SucceededToday("ok", day) {
		t.Fatal("有成功记录时应算成功")
	}
	// 关键：失败后又成功，整体算成功 ⇒ 不再重试
	_ = l.Append(checkin.Entry{AccountID: "fail", Day: day, At: now.Add(-30 * time.Minute).UnixMilli(), OK: true})
	if !l.SucceededToday("fail", day) {
		t.Fatal("失败后成功，应算成功")
	}
}

func TestLogMissingFileAttempts(t *testing.T) {
	l := checkin.OpenLog(filepath.Join(t.TempDir(), "nope.json"))
	if got := l.AttemptsToday("x", "2026-11-07"); got != 0 {
		t.Fatalf("文件不存在 want 0, got %d", got)
	}
	if l.SucceededToday("x", "2026-11-07") {
		t.Fatal("文件不存在不该算成功")
	}
}
