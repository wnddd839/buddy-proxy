package invariant

import (
	"fmt"
	"strings"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
	"github.com/wnddd839/codebuddy-proxy/internal/tokfp"
)

// I11：绝对参照闸门。
//
// ## 与 I9 / I10 的分工
//
//	I9  全局基线   → 「同一模型的比例有没有变」
//	I10 会话级对比 → 「某个会话中途有没有变」
//	I11 绝对参照   → 「这个比例是不是官方 DeepSeek 应有的值」
//
// 前两条都依赖**自建基线**：如果上游从一开始就跑的不是 DeepSeek，
// 基线会把「假的」当成基准，之后怎么变都不报警。
// I11 用官方词表标定的参考值做**外部锚定**，不依赖历史观测。
//
// ## 适用范围（必须守住）
//
// 只对**单一成分占比 ≥ 90%** 的请求生效。理由：混合文本的成分加权
// 估算有 7.26% 实测误差（见 tokfp/reference.go 的标定说明），
// 不足以支撑绝对比对。线上实测这类请求约占可用桶的 67%。
//
// 只对 **DeepSeek 家族**生效（模型名含 deepseek）。其他模型有自己的
// 分词器，拿 DeepSeek 的参考值去比毫无意义。
func registerTokAbs(r *Registry) {
	r.Add("I11", "比例必须符合 DeepSeek 官方词表的参考值",
		"自建基线会把「一开始就不是 DeepSeek」当成正常；绝对参照用官方词表标定的值做外部锚定",
		SeverityViolation, checkTokAbsoluteReference)
}

// dominantBucketShare 返回「被判定的那个桶」占比。
//
// 必须 >= 0.90 才参与 I11。少于这个比例时，混合文本的加权估算误差
// 会超过 I11 的容差，判定失去意义。
const dominantBucketShare = 0.90

// referenceRatioFor 返回该桶对应的官方参考比例。
//
// 注意 ascii 桶：reference 里 alpha 与 sym 相差 3 倍（0.2047 vs 0.6786），
// 而 Composition 不细分 ASCII。这里用「加权中间值」不成立（误差会翻倍），
// 所以直接**不给 ascii 桶判据** —— 宁可漏报，不可误报。
func referenceRatioFor(bucket string) (float64, bool) {
	switch bucket {
	case tokfp.BucketCJK:
		return tokfp.ReferenceCJK, true
	case tokfp.BucketDigits:
		return tokfp.ReferenceDigits, true
	default:
		return 0, false
	}
}

// isDeepSeekFamily 剥掉站点前缀后，名字里含 deepseek。
func isDeepSeekFamily(model string) bool {
	return strings.Contains(strings.ToLower(stripSitePrefix(model)), "deepseek")
}

// checkTokAbsoluteReference 用官方参考值比对比例。
func checkTokAbsoluteReference(rec decision.Record, _ *State) []decision.Finding {
	if rec.Kind != decision.KindRequest || !rec.Ok || rec.Canceled {
		return nil
	}
	if rec.TokRatio <= 0 || rec.TokChars <= 0 {
		return nil
	}
	if !tokRatioPlausible(rec.TokRatio) {
		return nil
	}
	if rec.TokBucket == tokfp.BucketTiny || rec.TokBucket == tokfp.BucketNone {
		return nil
	}

	model := rec.RoutedModel
	if model == "" {
		model = rec.Model
	}
	if !isDeepSeekFamily(model) {
		return nil
	}

	reference, ok := referenceRatioFor(rec.TokBucket)
	if !ok {
		return nil // ascii 桶不判（见 referenceRatioFor 的注释）
	}

	// 单一成分占比 >= 90% 才判。用全文长度做分母（采样只影响分类，
	// 不影响 TotalChars —— 但 Record 里只有 TokChars，那是分类字符数，
	// 采样时它只是前缀。所以这里用 TokChars 是有意的保守做法：
	// 采样记录的 TokChars 会偏小，占比可能被算高，从而**少判**。
	// 宁可漏报，不可误报。
	dominant := dominantCount(rec)
	if rec.TokChars <= 0 || float64(dominant)/float64(rec.TokChars) < dominantBucketShare {
		return nil
	}

	delta := rec.TokRatio - reference
	rel := delta / reference
	if rel < 0 {
		rel = -rel
	}
	if rel <= tokTolerance {
		return nil
	}

	return []decision.Finding{{
		Invariant: "I11",
		Severity:  SeverityViolation,
		Subject:   model + "|" + rec.TokBucket,
		At:        rec.At,
		Detail: fmt.Sprintf(
			"比例 %.4f 偏离官方 DeepSeek 参考值 %.4f 达 %.0f%%（容差 %.0f%%）",
			rec.TokRatio, reference, rel*100, tokTolerance*100),
		Why: "自建基线会把「一开始就不是 DeepSeek」当成正常；绝对参照不依赖历史观测",
		Evidence: map[string]any{
			"requestId": rec.RequestID,
			"model":     model,
			"bucket":    rec.TokBucket,
			"ratio":     rec.TokRatio,
			"reference": reference,
			"chars":     rec.TokChars,
		},
	}}
}

// dominantCount 返回与 bucket 对应的成分计数。
func dominantCount(rec decision.Record) int {
	switch rec.TokBucket {
	case tokfp.BucketCJK:
		return rec.TokCJK
	case tokfp.BucketDigits:
		return rec.TokDigits
	case tokfp.BucketASCII:
		return rec.TokASCII
	default:
		return 0
	}
}
