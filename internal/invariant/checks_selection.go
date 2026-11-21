package invariant

import (
	"fmt"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 选号断言 I7。
func registerSelection(r *Registry) {
	r.Add("I7", "每个被检查过的号都要有交代",
		"有账号被跳过却没有原因，就再也回答不出「那个号为什么没被用」",
		SeverityViolation, checkSelectionExplained)
}

// checkSelectionExplained 校验跳过原因是否覆盖了「实际检查过的号」。
//
// 判据用 Examined 而不是 Candidates：轮转选号在第一个可用号就返回，
// 剩下没看的号既不算跳过、也不该被要求交代；用总数判会稳定误报
// （健康池里 1/N 的请求会命中）。
//
// 没有 Examined（老记录）时退回 Candidates 语义，但只判「有跳过就必须有原因」，
// 不判覆盖率 —— 否则历史数据会刷出满屏假阳性。
func checkSelectionExplained(rec decision.Record, _ *State) []decision.Finding {
	if rec.Kind != decision.KindSelection {
		return nil
	}
	sum := 0
	for _, n := range rec.Skipped {
		sum += n
	}

	if rec.Chosen != "" {
		// 选中了号：被检查过的号里，除了选中的那个，其余都必须有原因。
		if rec.Examined > 1 && sum != rec.Examined-1 {
			return []decision.Finding{{
				Subject: rec.RequestID,
				Detail: fmt.Sprintf("检查了 %d 个号，选中 1 个，但只交代了 %d 个跳过原因",
					rec.Examined, sum),
				Evidence: map[string]any{
					"examined": rec.Examined, "chosen": rec.Chosen,
					"skipped": rec.Skipped, "site": rec.Site,
				},
			}}
		}
		return nil
	}

	// 全失败：每个被检查过的号都必须有交代。
	want := rec.Examined
	if want == 0 {
		want = rec.Candidates
	}
	if want > 0 && sum != want {
		return []decision.Finding{{
			Subject: rec.RequestID,
			Detail:  fmt.Sprintf("检查了 %d 个号，但跳过原因只覆盖 %d 个", want, sum),
			Evidence: map[string]any{
				"examined": rec.Examined, "candidates": rec.Candidates,
				"skipped": rec.Skipped, "site": rec.Site, "model": rec.Model,
			},
		}}
	}
	return nil
}
