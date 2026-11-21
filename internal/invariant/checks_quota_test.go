package invariant

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

func TestI1ClearNeedsWhitelistedReason(t *testing.T) {
	reg := DefaultRegistry()
	before := 5.0
	cases := []struct {
		op, reason string
		want       int
	}{
		{decision.OpClear, "", 1},
		{decision.OpClear, "随便写的", 1},
		{decision.OpClear, decision.ReasonQuotaRecovered, 0},
		{decision.OpClear, decision.ReasonInvalidSnapshot, 0},
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

// I2a 的四种情形。关键在于「上游收费但本地没扣」必须报 ——
// 那正是「扣减路径没跑」，也是评审 I-5 指出的自证漏洞。
func TestI2aCases(t *testing.T) {
	credit := 1.3
	zero := 0.0

	t.Run("上游收费且本地扣了一致", func(t *testing.T) {
		st := NewState(8, 8)
		st.PutDeduct("r1", 1.3, "")
		rec := decision.Record{Kind: decision.KindRequest, At: 2, RequestID: "r1", AccountID: "a1", Ok: true, Credit: &credit}
		if got := DefaultRegistry().Run(rec, st); len(got) != 0 {
			t.Fatalf("一致不该报：%+v", got)
		}
	})

	t.Run("上游收费但本地完全没记账", func(t *testing.T) {
		st := NewState(8, 8)
		rec := decision.Record{Kind: decision.KindRequest, At: 2, RequestID: "r2", AccountID: "a1", Ok: true, Credit: &credit}
		got := DefaultRegistry().Run(rec, st)
		if len(got) != 1 || got[0].Invariant != "I2a" {
			t.Fatalf("漏记应报：%+v", got)
		}
	})

	t.Run("上游收费但金额不一致", func(t *testing.T) {
		st := NewState(8, 8)
		st.PutDeduct("r3", 0.5, "")
		rec := decision.Record{Kind: decision.KindRequest, At: 2, RequestID: "r3", AccountID: "a1", Ok: true, Credit: &credit}
		if got := DefaultRegistry().Run(rec, st); len(got) != 1 || got[0].Invariant != "I2a" {
			t.Fatalf("不一致应报：%+v", got)
		}
	})

	t.Run("设计内跳过（额度未知）", func(t *testing.T) {
		st := NewState(8, 8)
		st.PutDeduct("r4", 0, decision.DeductSkipQuotaUnknown)
		rec := decision.Record{Kind: decision.KindRequest, At: 2, RequestID: "r4", AccountID: "a1", Ok: true, Credit: &credit}
		if got := DefaultRegistry().Run(rec, st); len(got) != 0 {
			t.Fatalf("额度未知时跳过扣减是设计内：%+v", got)
		}
	})

	t.Run("免费模型没有扣减", func(t *testing.T) {
		st := NewState(8, 8)
		st.PutDeduct("r5", 0, decision.DeductSkipNoCredit)
		rec := decision.Record{Kind: decision.KindRequest, At: 2, RequestID: "r5", AccountID: "a1", Ok: true, Credit: &zero}
		if got := DefaultRegistry().Run(rec, st); len(got) != 0 {
			t.Fatalf("免费模型不该报：%+v", got)
		}
	})

	t.Run("免费请求却扣了钱", func(t *testing.T) {
		st := NewState(8, 8)
		st.PutDeduct("r6", 0.9, "")
		rec := decision.Record{Kind: decision.KindRequest, At: 2, RequestID: "r6", AccountID: "a1", Ok: true, Credit: &zero}
		if got := DefaultRegistry().Run(rec, st); len(got) != 1 {
			t.Fatalf("多扣应报：%+v", got)
		}
	})

	t.Run("上游没回 credit 字段", func(t *testing.T) {
		st := NewState(8, 8)
		rec := decision.Record{Kind: decision.KindRequest, At: 2, RequestID: "r7", Ok: true}
		if got := DefaultRegistry().Run(rec, st); len(got) != 0 {
			t.Fatalf("无 credit 无从对账，不该报：%+v", got)
		}
	})

	t.Run("取消的请求不参与", func(t *testing.T) {
		st := NewState(8, 8)
		rec := decision.Record{Kind: decision.KindRequest, At: 2, RequestID: "r8", Ok: true, Canceled: true, Credit: &credit}
		if got := DefaultRegistry().Run(rec, st); len(got) != 0 {
			t.Fatalf("取消不该报：%+v", got)
		}
	})
}

func TestI2bNoDriftWhenAccounted(t *testing.T) {
	before, after := 100.0, 97.0
	match := decision.Record{
		Kind: decision.KindQuota, At: 3, AccountID: "a1",
		Op: decision.OpRefresh, Before: &before, After: &after, SpentSinceCheck: 3.0,
	}
	if got := DefaultRegistry().Run(match, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("不应误报：%+v", got)
	}

	drift := match
	drift.SpentSinceCheck = 1.0
	got := DefaultRegistry().Run(drift, NewState(8, 8))
	if len(got) != 1 || got[0].Severity != SeverityInfo {
		t.Fatalf("应出 info：%+v", got)
	}
}

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

func TestPanicIsContained(t *testing.T) {
	r := NewRegistry()
	r.Add("X1", "会 panic 的断言", "用来验证 recover", SeverityViolation,
		func(decision.Record, *State) []decision.Finding { panic("boom") })
	got := r.Run(decision.Record{At: 1}, NewState(8, 8))
	if len(got) != 1 || got[0].Subject != "invariant-panic" {
		t.Fatalf("panic 应被转成一条 finding：%+v", got)
	}
}

// 扣减记录必须有上限（评审 I-6：RequestID 不同源时是慢性泄漏）。
func TestDeductStateIsBounded(t *testing.T) {
	st := NewState(8, 8)
	st.SetDeductCap(3)
	for i := 0; i < 10; i++ {
		st.PutDeduct(string(rune('a'+i)), 1, "")
	}
	if got := st.PendingDeducts(); got != 3 {
		t.Fatalf("扣减记录应被限制在 3，实际 %d", got)
	}
	// 最早的已被淘汰
	if _, ok := st.TakeDeduct("a"); ok {
		t.Fatal("最旧的应被淘汰")
	}
	if _, ok := st.TakeDeduct("j"); !ok {
		t.Fatal("最新的应保留")
	}
}
