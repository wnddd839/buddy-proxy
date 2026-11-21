package invariant

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 有候选被跳过却没有原因 ⇒ 报。
func TestI7RequiresHistogram(t *testing.T) {
	reg := DefaultRegistry()

	bad := decision.Record{Kind: decision.KindSelection, At: 1, Candidates: 4, Chosen: "a1"}
	if got := reg.Run(bad, NewState(8, 8)); len(got) != 1 || got[0].Invariant != "I7" {
		t.Fatalf("缺原因应报：%+v", got)
	}

	good := decision.Record{
		Kind: decision.KindSelection, At: 1, Candidates: 4, Chosen: "a1",
		Skipped: map[string]int{"quota_floor": 2, "cooldown": 1},
	}
	if got := reg.Run(good, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("不该报：%+v", got)
	}
}

// 单候选时没有跳过是正常的。
func TestI7SingleCandidate(t *testing.T) {
	rec := decision.Record{Kind: decision.KindSelection, At: 1, Candidates: 1, Chosen: "a1"}
	if got := DefaultRegistry().Run(rec, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("单候选不该报：%+v", got)
	}
}

// 全不可选：计数对得上就不报，对不上就报。
func TestI7AllFailedNeedsFullCoverage(t *testing.T) {
	reg := DefaultRegistry()

	ok := decision.Record{
		Kind: decision.KindSelection, At: 1, Candidates: 2, Chosen: "",
		Skipped: map[string]int{"quota_floor": 2},
	}
	if got := reg.Run(ok, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("计数对得上不该报：%+v", got)
	}

	bad := decision.Record{
		Kind: decision.KindSelection, At: 1, Candidates: 3, Chosen: "",
		Skipped: map[string]int{"quota_floor": 1},
	}
	if got := reg.Run(bad, NewState(8, 8)); len(got) != 1 || got[0].Invariant != "I7" {
		t.Fatalf("计数对不上应报：%+v", got)
	}
}
