package concurrency

import (
	"testing"
	"time"
)

func TestBeginEndAndSnapshot(t *testing.T) {
	tr := New()
	end := tr.Begin("a", "账号A")
	if got := tr.Snapshot().GlobalInFlight; got != 1 {
		t.Fatalf("in-flight=%d want 1", got)
	}
	end(true)
	end(true) // 重复调用不应重复减计数
	snap := tr.Snapshot()
	if snap.GlobalInFlight != 0 || len(snap.Accounts) != 1 || snap.Accounts[0].InFlight != 0 {
		t.Fatalf("snapshot=%+v", snap)
	}
	if snap.Accounts[0].RPM1 != 1 {
		t.Fatalf("rpm1=%v want 1", snap.Accounts[0].RPM1)
	}
}

func TestSnapshotReportsWindowAverageRPM(t *testing.T) {
	tr := New()
	now := time.Now()
	st := tr.locked("a")
	for _, sample := range []struct {
		age      time.Duration
		requests int64
	}{
		{30 * time.Second, 3},
		{2 * time.Minute, 7},
		{10 * time.Minute, 20},
		{61 * time.Minute, 100},
	} {
		tr.bucketLocked(st, now.Add(-sample.age)).requests += sample.requests
	}
	view := tr.Snapshot().Accounts[0]
	if view.RPM1 != 3 || view.RPM5 != 2 || float64(view.RPM60) != 0.5 {
		t.Fatalf("window RPM: got %v/%v/%v, want 3/2/0.5", view.RPM1, view.RPM5, view.RPM60)
	}
}

func TestRecordRateLimitCapturesInFlight(t *testing.T) {
	tr := New()
	end := tr.Begin("a", "")
	tr.RecordRateLimit("a", "429")
	snap := tr.Snapshot()
	if len(snap.RateLimits) != 1 || snap.RateLimits[0].AccountInFlight != 1 {
		t.Fatalf("rate limits=%+v", snap.RateLimits)
	}
	end(false)
}

func TestSummaryIsNumeric(t *testing.T) {
	tr := New()
	end := tr.Begin("a", "")
	defer end(true)
	s := tr.Summary()
	if _, ok := s["accounts"].(int); !ok {
		t.Fatalf("accounts must be a number, got %T", s["accounts"])
	}
	if _, ok := s["rateLimits"].(int); !ok {
		t.Fatalf("rateLimits must be a number, got %T", s["rateLimits"])
	}
}

func TestNilTrackerIsSafe(t *testing.T) {
	var tr *Tracker
	end := tr.Begin("a", "")
	end(true)
	tr.RecordRateLimit("a", "429")
	if tr.Snapshot().GeneratedAt == 0 || len(tr.Summary()) == 0 {
		t.Fatal("nil tracker must be safe")
	}
}
