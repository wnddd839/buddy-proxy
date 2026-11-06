package concurrency

import "testing"

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
		t.Fatalf("rpm1=%d want 1", snap.Accounts[0].RPM1)
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
