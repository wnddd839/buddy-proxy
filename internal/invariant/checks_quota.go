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

// I2b：探针刷新必须与本地记账吻合 —— 只报「跌得比记账多」。
//
// ## 两条探针路径形状不同，判据必须同时容得下
//
//	ApplyQuotaProbe(真实 baseline) → after 已扣除 in-flight，after ≈ before
//	ApplyQuotaState(baseline=Inf)  → 不做 in-flight 校正，after ≈ before - spent
//
// 记录里没有标记是哪条路径，所以不能只判「after == before」，
// 也不能只判「after == before - spent」。
//
// ## 判据（四种情形都能分类）
//
//	drop = before - after                    （正数 = 余额下跌）
//	涨（drop < -tolerance）                  → 不报（签到/充值等外部因素）
//	跌但记账对得上（|drop-spent| <= tol）   → 不报
//	跌且记账对不上（|drop-spent| > tol）    → 报
//
// ## 实测依据（2026-11-22）
//
//	签到 +100：      145.2 → 245.2, spent=0   涨 ⇒ 不报
//	正常消耗：       100   → 97,    spent=3   对得上 ⇒ 不报
//	漏记一笔：       100   → 97,    spent=1   对不上 ⇒ 报
//	生产探针吻合：   162.36→162.36,spent=0.56 drop≈0 ⇒ 不报
func checkQuotaProbeDrift(rec decision.Record, _ *State) []decision.Finding {
	if rec.Kind != decision.KindQuota || rec.Op != decision.OpRefresh {
		return nil
	}
	if rec.Before == nil || rec.After == nil {
		return nil // 首次探针（没有基线）或清空（由 I1 管）
	}

	drop := *rec.Before - *rec.After
	// 上涨：签到、返积分、人工充值等外部因素，不可能是记账漏记。
	if drop < -driftTolerance {
		return nil
	}
	// 两条探针路径，任一匹配即算账对得上：
	//   |drop| <= tol           → ApplyQuotaProbe 路径（after 已扣除 in-flight）
	//   |drop - spent| <= tol   → ApplyQuotaState 路径（after 未校正）
	//
	// 单判任一条都会误伤另一条 —— 我上一版只判后者，结果
	// TestI2bComparesBeforeAfterDirectly（drop=0, spent=0.56）与
	// TestI2bToleratesFloatNoise（drop≈0, spent=4.27）两条生产实测值都报了。
	if math.Abs(drop) <= driftTolerance {
		return nil
	}
	if math.Abs(drop-rec.SpentSinceCheck) <= driftTolerance {
		return nil
	}

	return []decision.Finding{{
		Subject: rec.AccountID,
		Detail: fmt.Sprintf(
			"余额下跌 %.4g，本地记账 %.4g（差 %.4g，容差 %.4g）",
			drop, rec.SpentSinceCheck,
			drop-rec.SpentSinceCheck, driftTolerance),
		Evidence: map[string]any{
			"before":          *rec.Before,
			"after":           *rec.After,
			"drop":            drop,
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
