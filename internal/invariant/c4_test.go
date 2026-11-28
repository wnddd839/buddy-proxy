package invariant

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// C4 回归：走「清空额度快照」路径的请求，I2a 不该报「漏记」。
//
// ## 缺陷
//
// MarkResult 成功时：
//
//	if BypassedCooldown && wasQuotaBlocked && credit > 0 {
//	    clearQuotaCache(...)      // ← 清空，不扣减
//	} else {
//	    deductQuotaLocked(...)    // ← 扣减
//	}
//
// 走 clear 分支时没有扣减记录，而 I2a 看到 credit>0 且 TakeDeduct 无记录
// ⇒ 报「上游扣费 X，但没有对应的扣减记录」。这条路径每次成功都误报。
//
// 修法：clear 事件带 RequestID，Observer 为它记一条
// skipped=quota_cleared，I2a 见到该 skip 原因即豁免。
func TestI2aExemptsQuotaCleared(t *testing.T) {
	const cleared = "quota_cleared"

	cases := []struct {
		name    string
		credit  float64
		put     bool
		amount  float64
		skipped string
		want    bool // true = 应命中 I2a
	}{
		{
			name: "清空快照路径（设计内跳过）", credit: 2.5,
			put: true, amount: 0, skipped: cleared,
			want: false,
		},
		{
			name: "额度未知跳过", credit: 1.3,
			put: true, amount: 0, skipped: "quota_unknown",
			want: false,
		},
		{
			name: "真漏记（无任何记录）", credit: 1.3,
			put:  false,
			want: true,
		},
		{
			name: "真漏记（记 0 但无原因）", credit: 1.3,
			put: true, amount: 0, skipped: "",
			want: true,
		},
		{
			name: "金额不一致", credit: 1.3,
			put: true, amount: 0.5, skipped: "",
			want: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := NewState(8, 8)
			if c.put {
				st.PutDeduct("r1", c.amount, c.skipped)
			}
			credit := c.credit
			rec := decision.Record{
				Kind: decision.KindRequest, At: 1, RequestID: "r1",
				AccountID: "a1", Ok: true, Credit: &credit,
			}
			got := DefaultRegistry().Run(rec, st)
			hit := false
			for _, f := range got {
				if f.Invariant == "I2a" {
					hit = true
				}
			}
			if hit != c.want {
				t.Fatalf("%s：命中=%v，want %v\n%+v", c.name, hit, c.want, got)
			}
		})
	}
}
