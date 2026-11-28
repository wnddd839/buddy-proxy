package tokfp

import (
	"fmt"
	"strings"
)

// Verdict 是一条校验的结论。
type Verdict string

const (
	// VerdictPass 表示完全吻合。
	VerdictPass Verdict = "pass"
	// VerdictFail 表示明确不符（词表或模板不对）。
	VerdictFail Verdict = "fail"
	// VerdictUnknown 表示样本不足或上游没回 usage，无法判定。
	VerdictUnknown Verdict = "unknown"
)

// String 实现 fmt.Stringer（JSON 序列化与日志都直接用它）。
func (v Verdict) String() string { return string(v) }

// SampleResult 是一条标定样本的观测结果。
type SampleResult struct {
	Name string
	// ObservedTokens 是上游返回的 prompt_tokens（0 = 未观测）。
	ObservedTokens int
	// OfficialTokens 是该样本在官方 DeepSeek 词表下的精确计数。
	OfficialTokens int
	// Delta 是两者之差（含模板开销）。
	Delta int
	// Verdict 是这条样本单独判定的结果。
	Verdict Verdict
	// Note 是给人看的一句话说明。
	Note string
}

// IdentityReport 是一次词表身份鉴定的完整结果。
type IdentityReport struct {
	Model   string
	Samples []SampleResult
	// Vocabulary 是综合判定：所有样本的 delta 都等于模板开销 ⇒ 官方 DeepSeek 词表。
	Vocabulary Verdict
	// TemplateDelta 是各样本 delta 的统一值（不一致时为 0）。
	TemplateDelta int
	// TemplateConsistent 表示所有样本的 delta 相同 ——
	// 只有相同时模板开销才是个常数，词表判定才可信。
	TemplateConsistent bool
	Note               string
}

// AnalyzeIdentity 从实测值判定词表身份。
//
// ## 判据
//
// 官方 DeepSeek 词表 + 标准 chat template 下，
// 对**单条 user** 结构，`上游 prompt_tokens - 官方计数` 应当是一个常数
// （实测为 4，见 TemplateCases 的 1u 用例）。
//
// 关键在「常数」而不是「等于 4」：
//   - 若三项样本的 delta 各不相同 ⇒ 词表不同（每条样本的偏差量不一样）
//   - 若 delta 相同但不等于 4 ⇒ 词表可能相同但模板不同（例如多了系统前缀）
//
// 所以本函数先判「是否一致」，再判「是否等于预期」。
func AnalyzeIdentity(model string, observed []int) IdentityReport {
	rep := IdentityReport{Model: model}

	if len(CalibrationSamples) == 0 {
		rep.Vocabulary = VerdictUnknown
		rep.Note = "没有标定样本"
		return rep
	}

	// 单条 user 结构的预期模板开销。
	expected := FormattedOverhead([]TemplateMessage{
		{Role: "user", Content: TemplateProbeText},
	})

	seen := 0
	deltas := map[int]int{}
	for i, sample := range CalibrationSamples {
		obs := 0
		if i < len(observed) {
			obs = observed[i]
		}
		r := SampleResult{
			Name:           sample.Name,
			ObservedTokens: obs,
			OfficialTokens: sample.OfficialTokens,
		}
		if obs <= 0 {
			r.Verdict = VerdictUnknown
			r.Note = "未观测到 prompt_tokens"
			rep.Samples = append(rep.Samples, r)
			continue
		}
		r.Delta = obs - sample.OfficialTokens
		seen++
		deltas[r.Delta]++
		if r.Delta == expected {
			r.Verdict = VerdictPass
			r.Note = fmt.Sprintf("与官方词表完全吻合（差值 %d = 模板开销）", r.Delta)
		} else {
			r.Verdict = VerdictFail
			r.Note = fmt.Sprintf("偏离官方词表 %+d token", r.Delta)
		}
		rep.Samples = append(rep.Samples, r)
	}

	switch {
	case seen == 0:
		rep.Vocabulary = VerdictUnknown
		rep.Note = "没有可用的观测值"
		return rep
	case len(deltas) == 1:
		rep.TemplateConsistent = true
		for d := range deltas {
			rep.TemplateDelta = d
		}
		if rep.TemplateDelta == expected {
			rep.Vocabulary = VerdictPass
			rep.Note = "三项样本的差值完全一致且等于模板开销 ⇒ 词表是官方 DeepSeek"
		} else {
			// 差值一致但不等：词表可能相同，模板不同。
			rep.Vocabulary = VerdictFail
			rep.Note = fmt.Sprintf(
				"三项样本差值一致（%+d）但不等于预期的 %d ⇒ 词表可能相同，聊天模板不同",
				rep.TemplateDelta, expected)
		}
	default:
		rep.TemplateConsistent = false
		rep.Vocabulary = VerdictFail
		var parts []string
		for _, r := range rep.Samples {
			if r.ObservedTokens > 0 {
				parts = append(parts, fmt.Sprintf("%s %+d", r.Name, r.Delta))
			}
		}
		rep.Note = "各样本差值不一致（" + strings.Join(parts, "，") +
			"）⇒ 不是官方 DeepSeek 词表"
	}
	return rep
}

// VocabularyLabel 返回给人看的词表判定标签。
func (r IdentityReport) VocabularyLabel() string {
	switch r.Vocabulary {
	case VerdictPass:
		return "官方 DeepSeek 词表"
	case VerdictFail:
		return "非 DeepSeek 词表"
	default:
		return "无法判定"
	}
}

// WeightLabel 是权重（模型本体）的可验证性说明。
//
// 恒为「无法验证」：上游不返回 logprobs，而词表相同不能证明权重相同
// （蒸馏/微调模型可以继承词表）。这条必须显式展示，
// 不能留白 —— 留白会被读成「已验证通过」。
func (r IdentityReport) WeightLabel() string {
	return "无法验证（上游不提供 logprobs）"
}
