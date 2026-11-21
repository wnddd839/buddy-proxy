package invariant

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

func acct(v float64) *float64 { return &v }

func TestI1ClearNeedsWhitelistedReason(t *testing.T) {
	reg := DefaultRegistry()
	before := 5.0
	cases := []struct {
		op, reason string
		want       int
	}{
		{decision.OpClear, "", 1},
		{decision.OpClear, "随便写的", 1},
		{decision.OpClear, decision.ReasonProbeExpired, 0},
		{decision.OpClear, decision.ReasonAdminClear, 0},
		{decision.OpImport, "", 0},
		{decision.OpReset, "", 0},
		{decision.OpDeduct, "", 0}, // 不是清空
	}
	for _, c := range cases {
		t.Run(c.op+"/"+c.reason, func(t *testing.T) {
			rec := decision.Record{
				Kind: decision.KindQuota, At: 1, AccountID: "a1",
				Op: c.op, Before: &before, After: nil, Reason: c.reason,
			}
			if got := len(reg.Run(rec, NewState(8, 8))); got != c.want {
				t.Fatalf("got %d findings, want %d", got, c.want)
			}
		})
	}
}

// 对照：有理由的清空不得报，且「没变 nil」的扣减也不得报。
func TestI1IgnoresNormalMutations(t *testing.T) {
	reg := DefaultRegistry()
	before, after := 10.0, 8.7
	rec := decision.Record{
		Kind: decision.KindQuota, At: 1, AccountID: "a1",
		Op: decision.OpDeduct, Before: &before, After: &after,
	}
	if got := reg.Run(rec, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("普通扣减不该报：%+v", got)
	}
}

// 免费模型（credit=0）且本地没扣 ⇒ 不报；上游收费但本地扣 0 ⇒ 报。
func TestI2aDetectMismatch(t *testing.T) {
	// credit=1.3 但没有扣减记录 ⇒ 忽略（额度未知时跳过扣减，属设计内）
	st := NewState(8, 8)
	credit := 1.3
	rec := decision.Record{Kind: decision.KindRequest, At: 2, RequestID: "r0", Ok: true, Credit: &credit}
	if got := DefaultRegistry().Run(rec, st); len(got) != 0 {
		t.Fatalf("无扣减记录不该报：%+v", got)
	}

	// credit=1.3，本地只扣了 0 ⇒ 必须报（漏记一笔）
	st.PutDeduct("r1", 0)
	rec.RequestID = "r1"
	got := DefaultRegistry().Run(rec, st)
	if len(got) != 1 || got[0].Invariant != "I2a" {
		t.Fatalf("应命中 I2a：%+v", got)
	}

	// 对照：免费请求却记了费 ⇒ 也必须报（多扣）
	st.PutDeduct("r2", 1.0)
	zero := 0.0
	free := decision.Record{Kind: decision.KindRequest, At: 3, RequestID: "r2", Ok: true, Credit: &zero}
	got = DefaultRegistry().Run(free, st)
	if len(got) != 1 || got[0].Invariant != "I2a" {
		t.Fatalf("免费请求多扣应报：%+v", got)
	}
}

func TestI2aMatchesWhenEqual(t *testing.T) {
	st := NewState(8, 8)
	st.PutDeduct("r1", 1.3)
	credit := 1.3
	rec := decision.Record{Kind: decision.KindRequest, At: 2, RequestID: "r1", Ok: true, Credit: &credit}
	if got := DefaultRegistry().Run(rec, st); len(got) != 0 {
		t.Fatalf("相等不该报：%+v", got)
	}
}

// 免费模型（credit=0）不参与对账 —— 否则每次免费请求都会误报。
func TestI2aSkipsFreeModel(t *testing.T) {
	st := NewState(8, 8)
	zero := 0.0
	rec := decision.Record{Kind: decision.KindRequest, At: 2, RequestID: "r1", Ok: true, Credit: &zero}
	if got := DefaultRegistry().Run(rec, st); len(got) != 0 {
		t.Fatalf("免费模型不该报：%+v", got)
	}
	// 没有 credit 字段（上游没回）同样跳过。
	if got := DefaultRegistry().Run(decision.Record{Kind: decision.KindRequest, At: 2, Ok: true}, st); len(got) != 0 {
		t.Fatalf("无 credit 不该报：%+v", got)
	}
}

// 探针刷新的对照：in-flight 已被 ApplyQuotaProbe 校正过，账面应当吻合。
func TestI2bNoDriftWhenAccounted(t *testing.T) {
	before, after := 100.0, 97.0 // 差 3
	match := decision.Record{
		Kind: decision.KindQuota, At: 3, AccountID: "a1",
		Op: decision.OpRefresh, Before: &before, After: &after, SpentSinceCheck: 3.0,
	}
	if got := DefaultRegistry().Run(match, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("不应误报：%+v", got)
	}

	drift := match
	drift.SpentSinceCheck = 1.0 // 账面只记了 1，实际掉了 3
	got := DefaultRegistry().Run(drift, NewState(8, 8))
	if len(got) != 1 || got[0].Severity != SeverityInfo {
		t.Fatalf("应出 info：%+v", got)
	}
}

// 「额度由未知变有值」（首次探针）不是漂移。
func TestI2bSkipsFirstProbe(t *testing.T) {
	after := 50.0
	rec := decision.Record{
		Kind: decision.KindQuota, At: 3, AccountID: "a1",
		Op: decision.OpRefresh, Before: nil, After: &after,
	}
	if got := DefaultRegistry().Run(rec, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("首次探针不该报：%+v", got)
	}
}

// 每条注册的断言都必须带 why（否则面板 tooltip 为空，用户无从判断）。
func TestAllInvariantsHaveWhy(t *testing.T) {
	for _, spec := range DefaultRegistry().Specs() {
		if spec["why"] == "" {
			t.Errorf("%s 缺 why", spec["id"])
		}
		if spec["title"] == "" {
			t.Errorf("%s 缺 title", spec["id"])
		}
	}
}

// 断言自身 panic 不得穿出去影响请求路径。
func TestPanicIsContained(t *testing.T) {
	r := NewRegistry()
	r.Add("X1", "会 panic 的断言", "用来验证 recover", SeverityViolation,
		func(decision.Record, *State) []decision.Finding { panic("boom") })
	got := r.Run(decision.Record{At: 1}, NewState(8, 8))
	if len(got) != 1 || got[0].Subject != "invariant-panic" {
		t.Fatalf("panic 应被转成一条 finding：%+v", got)
	}
}
