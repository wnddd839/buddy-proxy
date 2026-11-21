package invariant

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 健康池的普通成功：检查了 1 个号就直接选中 ⇒ 没有任何跳过原因，不得报。
// 这是评审 C1 的场景（轮转选号 1/N 的请求会命中旧判据）。
func TestI7HealthyPoolNoSkip(t *testing.T) {
	rec := decision.Record{
		Kind: decision.KindSelection, At: 1,
		Candidates: 243, Examined: 1, Chosen: "a1",
	}
	if got := DefaultRegistry().Run(rec, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("健康池首次命中的号不该报：%+v", got)
	}
}

// 检查了 3 个、跳过 2 个、选中 1 个 ⇒ 原因必须覆盖这 2 个。
func TestI7RequiresCoverageOfExamined(t *testing.T) {
	good := decision.Record{
		Kind: decision.KindSelection, At: 1,
		Candidates: 10, Examined: 3, Chosen: "a3",
		Skipped: map[string]int{"quota_floor": 2},
	}
	if got := DefaultRegistry().Run(good, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("原因覆盖正确不该报：%+v", got)
	}

	bad := decision.Record{
		Kind: decision.KindSelection, At: 1,
		Candidates: 10, Examined: 3, Chosen: "a3",
		Skipped: map[string]int{"quota_floor": 1},
	}
	if got := DefaultRegistry().Run(bad, NewState(8, 8)); len(got) != 1 || got[0].Invariant != "I7" {
		t.Fatalf("少交代一个应报：%+v", got)
	}
}

// 全不可选：原因总数必须覆盖检查过的号。
func TestI7AllFailedNeedsFullCoverage(t *testing.T) {
	reg := DefaultRegistry()

	ok := decision.Record{
		Kind: decision.KindSelection, At: 1,
		Candidates: 5, Examined: 2, Chosen: "",
		Skipped: map[string]int{"quota_floor": 2},
	}
	if got := reg.Run(ok, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("覆盖对得上不该报：%+v", got)
	}

	// 含禁用号：candidates 是全部账号数，examined 只数真正检查的。
	mixed := decision.Record{
		Kind: decision.KindSelection, At: 1,
		Candidates: 6, Examined: 3, Chosen: "",
		Skipped: map[string]int{"disabled": 1, "quota_floor": 2},
	}
	if got := reg.Run(mixed, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("含禁用号的池不该报：%+v", got)
	}

	// 老记录（无 Examined）退回 Candidates 语义。
	legacy := decision.Record{
		Kind: decision.KindSelection, At: 1,
		Candidates: 3, Chosen: "",
		Skipped: map[string]int{"quota_floor": 1},
	}
	if got := reg.Run(legacy, NewState(8, 8)); len(got) != 1 {
		t.Fatalf("老记录覆盖不足应报：%+v", got)
	}
}

// 单候选、无跳过：不该报。
func TestI7SingleCandidate(t *testing.T) {
	rec := decision.Record{Kind: decision.KindSelection, At: 1, Candidates: 1, Examined: 1, Chosen: "a1"}
	if got := DefaultRegistry().Run(rec, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("单候选不该报：%+v", got)
	}
}
