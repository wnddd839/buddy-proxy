package systemprompt

import (
	"testing"
	"time"
)

func TestJournalKeepsNewestFirst(t *testing.T) {
	j := NewJournal(3)
	for i := 1; i <= 5; i++ {
		j.Record(Event{At: int64(i), Status: 200})
	}
	got := j.Recent(10)
	if len(got) != 3 {
		t.Fatalf("容量 3 应只留 3 条，实际 %d", len(got))
	}
	if got[0].At != 5 || got[2].At != 3 {
		t.Fatalf("应为最新在前 5,4,3，实际 %d,%d,%d", got[0].At, got[1].At, got[2].At)
	}
}

func TestJournalRecentLimit(t *testing.T) {
	j := NewJournal(10)
	for i := 1; i <= 6; i++ {
		j.Record(Event{At: int64(i), Status: 200})
	}
	if got := j.Recent(2); len(got) != 2 || got[0].At != 6 {
		t.Fatalf("Recent(2) 应返回最新 2 条，实际 %v", got)
	}
	if got := j.Recent(0); len(got) != 0 {
		t.Fatalf("Recent(0) 应返回空，实际 %d 条", len(got))
	}
}

func TestJournalRecentDoesNotAlias(t *testing.T) {
	j := NewJournal(5)
	j.Record(Event{At: 1, Status: 200, Model: "m1"})
	got := j.Recent(1)
	got[0].Model = "mutated"
	again := j.Recent(1)
	if again[0].Model != "m1" {
		t.Fatalf("Recent 应返回副本，外部修改影响了内部状态：%q", again[0].Model)
	}
}

func TestJournalDefaultCapacity(t *testing.T) {
	j := NewJournal(0)
	for i := 0; i < 600; i++ {
		j.Record(Event{At: int64(i), Status: 200})
	}
	got := j.Recent(1000)
	if len(got) != 500 {
		t.Fatalf("默认容量应为 500，实际 %d", len(got))
	}
	if got[0].At != 599 {
		t.Fatalf("最新一条应为 599，实际 %d", got[0].At)
	}
}

func TestJournalStats24h(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	j := NewJournal(50)
	j.Record(Event{At: now.UnixMilli(), Form: "keep", Status: 200})
	j.Record(Event{At: now.UnixMilli(), Form: "fold", Status: 200})
	// 真实链路里「降级」是 keep 被 11128 打回那一条，随后才是 fold/200。
	j.Record(Event{At: now.UnixMilli(), Form: "keep", Status: 502, ErrorCode: "11128", Degraded: true})
	j.Record(Event{At: now.UnixMilli(), Form: "fold", Status: 0})
	// 25 小时前的旧事件不计入
	j.Record(Event{At: now.Add(-25 * time.Hour).UnixMilli(), Form: "keep", Status: 200})

	s := j.Stats24h(now)
	if s.Total != 4 {
		t.Fatalf("Total=%d want 4", s.Total)
	}
	if s.Keep != 2 || s.Fold != 2 {
		t.Fatalf("Keep=%d Fold=%d want 2/2", s.Keep, s.Fold)
	}
	if s.Degraded != 1 {
		t.Fatalf("Degraded=%d want 1", s.Degraded)
	}
	if s.Blocked != 1 {
		t.Fatalf("Blocked=%d want 1", s.Blocked)
	}
	if s.OK != 2 || s.Failed != 2 {
		t.Fatalf("OK=%d Failed=%d want 2/2", s.OK, s.Failed)
	}
}

func TestJournalClear(t *testing.T) {
	j := NewJournal(5)
	j.Record(Event{At: 1, Status: 200})
	j.Clear()
	if got := j.Recent(10); len(got) != 0 {
		t.Fatalf("Clear 后应为空，实际 %d 条", len(got))
	}
}

func TestJournalNilSafe(t *testing.T) {
	var j *Journal
	j.Record(Event{At: 1, Status: 200})
	if got := j.Recent(10); len(got) != 0 {
		t.Fatalf("nil journal 应返回空，实际 %d", len(got))
	}
	j.Clear()
	if got := j.Stats24h(time.Now()); got.Total != 0 {
		t.Fatalf("nil journal 统计应为零值，实际 %+v", got)
	}
}
