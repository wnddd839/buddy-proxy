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

	r.Add("I2a", "扣减额必须等于上游实际扣费",
		"上游收 1.3 而本地扣 0（或扣 2），就是漏记或多扣；两者都会让余额与真实脱节",
		SeverityViolation, checkQuotaDeductMismatch)

	r.Add("I2b", "探针刷新与本地账本必须吻合",
		"探针已按在途请求校正过余额，仍有偏差说明记账漏了一笔",
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

// I2a：本地扣减额必须与上游 credit 一致。
//
// 三种情形：
//   - credit 缺失（nil）：上游没回扣费字段，无从对账 ⇒ 跳过
//   - credit == 0（免费模型）：本地**不得**有任何扣减；有就是给免费请求记了费
//   - credit > 0：本地扣减额必须等于它（±1e-6）
func checkQuotaDeductMismatch(rec decision.Record, state *State) []decision.Finding {
	if rec.Kind != decision.KindRequest || !rec.Ok || rec.Canceled {
		return nil
	}
	if rec.Credit == nil || state == nil || rec.RequestID == "" {
		return nil
	}
	deducted, ok := state.TakeDeduct(rec.RequestID)
	if *rec.Credit <= 0 {
		if !ok {
			return nil // 免费请求没有扣减，正确
		}
		return []decision.Finding{{
			Subject: rec.AccountID,
			Detail: fmt.Sprintf("上游未扣费（credit=0），本地却扣了 %s",
				formatFloat(&deducted)),
			Evidence: map[string]any{
				"requestId": rec.RequestID, "upstream": *rec.Credit, "deducted": deducted,
			},
		}}
	}
	if !ok {
		// 没有扣减记录：可能是额度未知（nil）跳过扣减，属设计内，
		// 不算 mismatch —— 由 I1 负责盯「额度变未知」这件事。
		return nil
	}
	if math.Abs(deducted-*rec.Credit) <= 1e-6 {
		return nil
	}
	return []decision.Finding{{
		Subject: rec.AccountID,
		Detail: fmt.Sprintf("上游扣费 %s，本地扣了 %s（差 %s）",
			formatFloat(rec.Credit), formatFloat(&deducted), formatFloat(ptr(math.Abs(deducted-*rec.Credit)))),
		Evidence: map[string]any{
			"requestId": rec.RequestID,
			"upstream":  *rec.Credit,
			"deducted":  deducted,
		},
	}}
}

// I2b：探针刷新后的余额差应与本地累计消耗吻合。
func checkQuotaProbeDrift(rec decision.Record, _ *State) []decision.Finding {
	if rec.Kind != decision.KindQuota || rec.Op != decision.OpRefresh {
		return nil
	}
	if rec.Before == nil || rec.After == nil {
		return nil // 首次探针（没有基线）或清空（由 I1 管）
	}
	observed := *rec.Before - *rec.After
	drift := observed - rec.SpentSinceCheck
	if math.Abs(drift) <= 0.5 {
		return nil
	}
	return []decision.Finding{{
		Subject: rec.AccountID,
		Detail: fmt.Sprintf("余额实际变化 %s，本地账本只记了 %s（差 %s）",
			formatFloat(ptr(observed)), formatFloat(ptr(rec.SpentSinceCheck)), formatFloat(ptr(drift))),
		Evidence: map[string]any{
			"observed":        observed,
			"spentSinceCheck": rec.SpentSinceCheck,
			"drift":           drift,
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
