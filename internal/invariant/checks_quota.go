package invariant

import (
	"fmt"
	"math"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 额度域断言 I1 / I2a / I2b。
func registerQuota(r *Registry) {
	r.Add("I1", "额度不能无声变成未知",
		"nil 语义是「未知 → 放行」，无理由清空等于撤销一切额度保护",
		SeverityViolation, checkQuotaNoSilentUnknown)

	r.Add("I2a", "上游收费时本地必须记账",
		"上游扣了钱而本地没动额度，余额就会与真实脱节，最终把号打穿",
		SeverityViolation, checkQuotaDeductMismatch)

	r.Add("I2b", "探针余额与本地快照必须吻合",
		"探针已按在途请求校正过回包，若与本地快照仍有差，说明有一笔记账漏了",
		SeverityInfo, checkQuotaProbeDrift)
}

// I1：余额由非 nil 变 nil 必须有白名单理由。
func checkQuotaNoSilentUnknown(rec decision.Record, _ *State) []decision.Finding {
	if rec.Kind != decision.KindQuota || rec.Before == nil || rec.After != nil {
		return nil
	}
	switch rec.Op {
	case decision.OpImport, decision.OpReset:
		return nil // 导入/复位不是「运行时状态转移」，清空是预期
	case decision.OpClear, decision.OpRefresh:
	default:
		return nil
	}
	if _, ok := decision.AllowedClearReasons[rec.Reason]; ok {
		return nil
	}
	return []decision.Finding{{
		Subject: rec.AccountID,
		Detail: fmt.Sprintf("额度从 %s 清成未知（op=%s reason=%q）",
			formatFloat(rec.Before), rec.Op, rec.Reason),
		Evidence: map[string]any{
			"op": rec.Op, "reason": rec.Reason,
			"before": *rec.Before, "spentSinceCheck": rec.SpentSinceCheck,
		},
	}}
}

// I2a：上游收费时，本地必须真的扣了账，且扣减量必须等于上游。
func checkQuotaDeductMismatch(rec decision.Record, state *State) []decision.Finding {
	if rec.Kind != decision.KindRequest || !rec.Ok || rec.Canceled {
		return nil
	}
	if rec.Credit == nil || state == nil || rec.RequestID == "" {
		return nil
	}
	dr, ok := state.TakeDeduct(rec.RequestID)

	if *rec.Credit <= 0 {
		if !ok || dr.Amount == 0 {
			return nil // 免费请求没有扣减，正确
		}
		return []decision.Finding{{
			Subject: rec.AccountID,
			Detail: fmt.Sprintf("上游未扣费（credit=0），本地却扣了 %s",
				formatFloat(ptr(dr.Amount))),
			Evidence: map[string]any{
				"requestId": rec.RequestID, "upstream": *rec.Credit, "deducted": dr.Amount,
			},
		}}
	}

	if !ok {
		return []decision.Finding{{
			Subject: rec.AccountID,
			Detail: fmt.Sprintf("上游扣费 %s，但没有对应的扣减记录",
				formatFloat(rec.Credit)),
			Evidence: map[string]any{"requestId": rec.RequestID, "upstream": *rec.Credit},
		}}
	}
	if dr.Skipped != "" {
		return nil // 设计内跳过（额度未知 / 不限量 / 无扣费）
	}
	if math.Abs(dr.Amount-*rec.Credit) <= 1e-6 {
		return nil
	}
	return []decision.Finding{{
		Subject: rec.AccountID,
		Detail: fmt.Sprintf("上游扣费 %s，本地扣了 %s（差 %s）",
			formatFloat(rec.Credit), formatFloat(ptr(dr.Amount)),
			formatFloat(ptr(math.Abs(dr.Amount-*rec.Credit)))),
		Evidence: map[string]any{
			"requestId": rec.RequestID,
			"upstream":  *rec.Credit,
			"deducted":  dr.Amount,
		},
	}}
}

// driftTolerance 是探针对账的宽容度。
//
// 上游回包是浮点运算结果，实测会带 1e-14 量级的噪声；同时探针与在途
// 请求之间有几毫秒的竞态窗口，可能差一笔很小的扣费。0.5 credit 足以
// 吸收这两者，又远小于任何真实的「漏记一笔」（通常 0.1~5 credit）。
const driftTolerance = 0.5

// I2b：探针回包与本地快照必须吻合。
//
// 关键：Before 与 After 是**同一口径** —— 都是「已扣过在途消耗」的
// 本地余额视图。ApplyQuotaProbe 在写入前已经把 inFlightSpent 从上游
// 回包里减掉了，所以两者相等就说明账对得上。
//
// SpentSinceCheck 只是**证据**（说明这段时间本地记了多少），
// 不能再从差额里减一次 —— 那样等于把它算两遍（上线首分钟即误报）。
func checkQuotaProbeDrift(rec decision.Record, _ *State) []decision.Finding {
	if rec.Kind != decision.KindQuota || rec.Op != decision.OpRefresh {
		return nil
	}
	if rec.Before == nil || rec.After == nil {
		return nil // 首次探针（没有基线）或清空（由 I1 管）
	}
	drift := *rec.After - *rec.Before
	if math.Abs(drift) <= driftTolerance {
		return nil
	}
	return []decision.Finding{{
		Subject: rec.AccountID,
		Detail: fmt.Sprintf("探针余额 %s 与本地快照 %s 相差 %s（同期本地记账 %s）",
			formatFloat(rec.After), formatFloat(rec.Before),
			formatFloat(ptr(drift)), formatFloat(ptr(rec.SpentSinceCheck))),
		Evidence: map[string]any{
			"before":          *rec.Before,
			"after":           *rec.After,
			"drift":           drift,
			"spentSinceCheck": rec.SpentSinceCheck,
		},
	}}
}

func formatFloat(v *float64) string {
	if v == nil {
		return "unknown"
	}
	return fmt.Sprintf("%.4g", *v)
}

func ptr(v float64) *float64 { return &v }
