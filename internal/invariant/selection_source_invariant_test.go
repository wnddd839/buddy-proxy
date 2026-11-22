package invariant

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// pin 路径（会话软钉 / 固定 Key）只检查 1 个号、不跳过任何人，
// 不得因为「池里候选很多但没有跳过原因」而命中 I7。
//
// 实测背景：上线后 45 分钟，55 条 request 只有 8 条 selection ——
// 因为软钉走的是「指定账号」分支，那条分支原本完全不发事件。
// 补上事件之后，必须确保 I7 不会因此误报。
func TestI7SilentOnPinPath(t *testing.T) {
	rec := decision.Record{
		Kind: decision.KindSelection, At: 1,
		RequestID: "r-pin", Site: "global", Model: "m",
		SelSource:  "pin",
		Candidates: 243, Examined: 1, Chosen: "a1",
	}
	if got := DefaultRegistry().Run(rec, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("pin 路径不该命中 I7：%+v", got)
	}
}

// 三条路径的 Source 都要能落进 decision.Record（回放时能区分）。
func TestSelectionSourceSurvivesRecord(t *testing.T) {
	for _, src := range []string{"pin", "prefer_quota", "rotation"} {
		rec := decision.Record{
			Kind: decision.KindSelection, At: 1,
			RequestID: "r", Examined: 1, Chosen: "a1", SelSource: src,
		}
		if got := DefaultRegistry().Run(rec, NewState(8, 8)); len(got) != 0 {
			t.Errorf("source=%s 不该命中：%+v", src, got)
		}
	}
}
