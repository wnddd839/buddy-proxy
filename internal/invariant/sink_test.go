package invariant

import (
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

func newTestSink(t *testing.T, ringSize, queueSize int) (*Sink, *Registry) {
	t.Helper()
	st, err := decision.OpenStore(t.TempDir(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	reg.Add("T1", "测试用断言", "用来验证 sink 行为", SeverityViolation,
		func(rec decision.Record, _ *State) []decision.Finding {
			if rec.Kind != "trigger" {
				return nil
			}
			return []decision.Finding{{Subject: rec.AccountID, Detail: "hit"}}
		})
	return NewSink(st, reg, NewState(8, 8), ringSize, queueSize), reg
}

// 队列满时必须丢弃并计数，绝不能让提交方阻塞。
func TestSinkDropsInsteadOfBlocking(t *testing.T) {
	s, _ := newTestSink(t, 8, 1)
	// 故意不 Start：写协程不在，队列容量 1 之后必然满。
	done := make(chan struct{})
	go func() {
		for i := 0; i < 5; i++ {
			s.Submit(decision.Record{Kind: "trigger", AccountID: "a" + string(rune('0'+i))})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Submit 阻塞了")
	}
	if s.Dropped() == 0 {
		t.Fatal("队列满时应计数丢弃")
	}
}

// 同一 (invariant, subject) 在窗口内只保留一条，其余计入 Suppressed。
func TestSinkDedupesWithinWindow(t *testing.T) {
	s, _ := newTestSink(t, 8, 64)
	s.Start()
	defer s.Stop()

	now := time.Now().UnixMilli()
	for i := 0; i < 3; i++ {
		s.Submit(decision.Record{Kind: "trigger", At: now + int64(i), AccountID: "same"})
	}
	got := s.Findings(10)
	if len(got) != 1 {
		t.Fatalf("应只保留 1 条，实际 %d", len(got))
	}
	if got[0].Suppressed != 2 {
		t.Fatalf("Suppressed 应为 2，实际 %d", got[0].Suppressed)
	}
	if s.Suppressed() != 2 {
		t.Fatalf("总 Suppressed 应为 2，实际 %d", s.Suppressed())
	}
}

// 不同 subject 各自保留。
func TestSinkDedupesPerSubject(t *testing.T) {
	s, _ := newTestSink(t, 8, 64)
	s.Start()
	defer s.Stop()
	now := time.Now().UnixMilli()
	s.Submit(decision.Record{Kind: "trigger", At: now, AccountID: "a"})
	s.Submit(decision.Record{Kind: "trigger", At: now, AccountID: "b"})
	if got := s.Findings(10); len(got) != 2 {
		t.Fatalf("不同 subject 应各自保留，实际 %d", len(got))
	}
}

// 超出窗口后允许再次落一条。
func TestSinkAllowsAfterWindow(t *testing.T) {
	s, _ := newTestSink(t, 8, 64)
	s.Start()
	defer s.Stop()
	now := time.Now().UnixMilli()
	s.Submit(decision.Record{Kind: "trigger", At: now - dedupeWindow.Milliseconds() - 1, AccountID: "x"})
	s.Submit(decision.Record{Kind: "trigger", At: now, AccountID: "x"})
	if got := s.Findings(10); len(got) != 2 {
		t.Fatalf("窗口外应再落一条，实际 %d", len(got))
	}
}

// ring 满后淘汰最旧，长度不超上限。
func TestSinkRingIsBounded(t *testing.T) {
	s, _ := newTestSink(t, 3, 64)
	s.Start()
	defer s.Stop()
	now := time.Now().UnixMilli()
	for i := 0; i < 10; i++ {
		s.Submit(decision.Record{Kind: "trigger", At: now + int64(i), AccountID: string(rune('a' + i))})
	}
	got := s.Findings(0)
	if len(got) != 3 {
		t.Fatalf("ring 应被限制在 3，实际 %d", len(got))
	}
	// 最新在前
	if got[0].Subject != "j" {
		t.Fatalf("最新应在最前，实际 %q", got[0].Subject)
	}
}

// 回扫必须重建计数，这样重启后「0 命中」与「没发生」才可区分。
func TestBackfillRebuildsCounts(t *testing.T) {
	st, err := decision.OpenStore(t.TempDir(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, id := range []string{"A", "B"} {
		f := decision.Finding{Invariant: "I3", Severity: SeverityViolation, Subject: id, At: now.Add(-time.Hour).UnixMilli(), Detail: "x"}
		if err := st.Append(decision.Record{Kind: decision.KindFinding, At: f.At, Finding: &f}); err != nil {
			t.Fatal(err)
		}
	}
	s := NewSink(st, NewRegistry(), NewState(8, 8), 200, 64)
	if err := s.Backfill(now); err != nil {
		t.Fatal(err)
	}
	if got := s.Findings(10); len(got) != 2 {
		t.Fatalf("回扫应恢复 2 条，实际 %d", len(got))
	}
	counts := s.Counts(now)
	if counts["I3"] != 2 {
		t.Fatalf("计数应为 2，实际 %v", counts)
	}
}

// 24 小时之前的 finding 不进回扫。
func TestBackfillIgnoresOld(t *testing.T) {
	st, err := decision.OpenStore(t.TempDir(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	f := decision.Finding{Invariant: "I3", Subject: "old", At: now.Add(-48 * time.Hour).UnixMilli(), Detail: "x"}
	if err := st.Append(decision.Record{Kind: decision.KindFinding, At: f.At, Finding: &f}); err != nil {
		t.Fatal(err)
	}
	s := NewSink(st, NewRegistry(), NewState(8, 8), 200, 64)
	if err := s.Backfill(now); err != nil {
		t.Fatal(err)
	}
	if got := s.Findings(10); len(got) != 0 {
		t.Fatalf("48h 前的不该回扫进来，实际 %d", len(got))
	}
}

// 注入 logger 时每条去重后的 finding 都要发一条日志。
func TestSinkEmitsLog(t *testing.T) {
	s, _ := newTestSink(t, 8, 64)
	var logs []string
	s.SetLogger(func(msg string, fields map[string]any) {
		logs = append(logs, msg+":"+fields["invariant"].(string))
	})
	s.Start()
	defer s.Stop()
	s.Submit(decision.Record{Kind: "trigger", AccountID: "z"})
	if len(logs) != 1 || logs[0] != "invariant hit:T1" {
		t.Fatalf("日志不对：%v", logs)
	}
}
