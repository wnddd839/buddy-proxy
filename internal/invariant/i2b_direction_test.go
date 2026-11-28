package invariant

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// I2b 只该报「余额跌得比记账多」（说明漏记一笔）。
//
// 实测踩到的误报（2026-11-22 部署后）：
//
//	账号 f53d969f 签到成功 +100 积分 → 余额 145.2 → 245.2
//	I2b 报「探针余额 245.2 与本地快照 145.2 相差 100（同期本地记账 0）」
//
// 签到、返积分、人工充值都是**外部因素**，余额上涨永远不该判为记账漏记。
// 原来用 math.Abs(drift) 把涨跌都算了进去 —— 这是断言本身写得不严谨。
func TestI2bOnlyReportsDownwardDrift(t *testing.T) {
	cases := []struct {
		name     string
		before   float64
		after    float64
		spent    float64
		wantHits int
		why      string
	}{
		{
			name: "签到充值 +100", before: 145.2, after: 245.2, spent: 0,
			wantHits: 0, why: "余额上涨是外部因素（签到/充值），不是记账漏记",
		},
		{
			name: "正常消耗 3", before: 100, after: 97, spent: 3,
			wantHits: 0, why: "账对得上",
		},
		{
			name: "漏记一笔（跌 3 但只记 1）", before: 100, after: 97, spent: 1,
			wantHits: 1, why: "跌得比记账多 ⇒ 漏记",
		},
		{
			name: "小额下跌在容差内", before: 100, after: 99.7, spent: 0,
			wantHits: 0, why: "0.3 < 0.5 容差",
		},
		{
			name: "浮点噪声", before: 154.22000024999997, after: 154.22000025, spent: 0,
			wantHits: 0, why: "1e-14 量级不算漂移",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before, after := c.before, c.after
			rec := decision.Record{
				Kind: decision.KindQuota, At: 1, AccountID: "a1",
				Op: decision.OpRefresh, Before: &before, After: &after,
				SpentSinceCheck: c.spent,
			}
			got := DefaultRegistry().Run(rec, NewState(8, 8))
			hits := 0
			for _, f := range got {
				if f.Invariant == "I2b" {
					hits++
				}
			}
			if hits != c.wantHits {
				t.Fatalf("%s：命中 %d 次，want %d（%s）\n%+v",
					c.name, hits, c.wantHits, c.why, got)
			}
		})
	}
}
