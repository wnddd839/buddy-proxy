package invariant

import (
	"fmt"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 选号断言 I7。
func registerSelection(r *Registry) {
	r.Add("I7", "选号必须交代每个号为什么没被选",
		"有账号被跳过却没有原因，就再也回答不出「那个号为什么没被用」",
		SeverityViolation, checkSelectionExplained)
}

// checkSelectionExplained 校验跳过原因直方图与候选数是否自洽。
func checkSelectionExplained(rec decision.Record, _ *State) []decision.Finding {
	if rec.Kind != decision.KindSelection {
		return nil
	}
	sum := 0
	for _, n := range rec.Skipped {
		sum += n
	}

	if rec.Chosen != "" {
		// 选到了号：候选多于 1 个却没有任何跳过原因 ⇒ 必然悄悄跳过了谁。
		if rec.Candidates > 1 && sum == 0 {
			return []decision.Finding{{
				Subject: rec.RequestID,
				Detail:  fmt.Sprintf("候选 %d 个、选中 1 个，但没有任何跳过原因", rec.Candidates),
				Evidence: map[string]any{
					"candidates": rec.Candidates, "chosen": rec.Chosen, "site": rec.Site,
				},
			}}
		}
		return nil
	}

	// 全失败：每个候选都必须有交代，否则说明有一个号被跳过后没记账。
	if sum != rec.Candidates {
		return []decision.Finding{{
			Subject: rec.RequestID,
			Detail:  fmt.Sprintf("候选 %d 个，但跳过原因只覆盖 %d 个", rec.Candidates, sum),
			Evidence: map[string]any{
				"candidates": rec.Candidates, "skipped": rec.Skipped, "site": rec.Site, "model": rec.Model,
			},
		}}
	}
	return nil
}
