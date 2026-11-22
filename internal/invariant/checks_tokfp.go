package invariant

import (
	"fmt"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
	"github.com/wnddd839/codebuddy-proxy/internal/tokfp"
)

// I9：分词器指纹突变（模型被悄悄换掉的物理证据）。
//
// ## 为什么需要它（I8 覆盖不到的场景）
//
// I8 只看上游**回显**的 model 字段。上游老实回显时它立刻能发现问题；
// 但若回显仍是期望的名字、实际却换了模型（或有中间层按指纹路由），
// 回显完全看不出。
//
// token 比例是改不了的物理证据。实测（2026-11-22，同账号同 prompt）：
//
//	模型                 zh       code     nums
//	deepseek-v4.1-flash  0.6371   0.3073   0.4552
//	glm-5.3-flash        0.6689   0.3217   0.6018   ← 数字密集差 32%
//	hy4flash/hy4std      0.6100   0.3098   0.4577
//
// 数字密集样本最敏感；中文/代码差 5%，需要基线看趋势。
//
// ## 判据
//
// 每个 (模型, 分桶) 维护快/慢双基线；新观测超出区间 8% 即报。
// 前 3 次只建立基线不判定 —— 样本太少时任何阈值都会误报。
func registerTokFP(r *Registry) {
	r.Add("I9", "分词器指纹不得突变",
		"同一模型对同类文本切出的 token 比例应当稳定；突变说明实际跑的模型被换了，而回显字段可能仍是原模型名",
		SeverityViolation, checkTokFingerprint)
}

// tokObservation 是一次观测的结果，供 I9 的两个分支共用。
//
// **必须只观测一次**：State 的样本计数是有状态的，两个 check 各调一次
// 会让样本数翻倍、基线区间立刻建好、后续阈值判定全部错乱。
// 所以这里用一条 check 完成「累积 + 判定」，两种 finding 从同一结果产出。
type tokObservation struct {
	model    string
	bucket   string
	ratio    float64
	base     tokBaseline
	ready    bool
	justMade bool // 本次恰好达到判定所需样本数
}

func observeTokOnce(rec decision.Record, st *State) (tokObservation, bool) {
	if rec.Kind != decision.KindRequest || !rec.Ok || rec.Canceled {
		return tokObservation{}, false
	}
	if rec.TokRatio <= 0 || rec.TokBucket == "" || st == nil {
		return tokObservation{}, false
	}
	if rec.TokBucket == tokfp.BucketTiny || rec.TokBucket == tokfp.BucketNone {
		return tokObservation{}, false
	}
	model := rec.RoutedModel
	if model == "" {
		model = rec.Model
	}
	if model == "" {
		return tokObservation{}, false
	}
	base, ready := st.ObserveTokRatio(model, rec.TokBucket, rec.TokRatio)
	// 值副本：ObserveTokRatio 已按值返回，这里不再持有 map 内指针。
	if base.Samples == 0 {
		return tokObservation{}, false
	}
	return tokObservation{
		model: model, bucket: rec.TokBucket, ratio: rec.TokRatio,
		base: base, ready: ready,
		// 样本数恰好等于判定阈值 ⇒ 基线刚建立完成。
		justMade: ready && base.Samples == tokMinSamples,
	}, true
}

// checkTokFingerprint 一条 check 同时产出两种 finding：
//   - I9  violation：比例偏离基线
//   - I9b info    ：基线刚建立（只报一次，用于确认观测生效）
func checkTokFingerprint(rec decision.Record, st *State) []decision.Finding {
	obs, ok := observeTokOnce(rec, st)
	if !ok {
		return nil
	}

	var out []decision.Finding

	// I9b：基线刚建立时报一次。
	if obs.justMade {
		out = append(out, decision.Finding{
			Invariant: "I9b",
			Severity:  SeverityInfo,
			Subject:   obs.model + "|" + obs.bucket,
			At:        rec.At,
			Detail: fmt.Sprintf("已建立分词器基线：%.4f（%d 个样本，区间 [%.4f, %.4f]）",
				obs.base.LastRatio, obs.base.Samples, obs.base.Fast, obs.base.Slow),
			Why: "基线建立后，后续同类请求的比例突变才能被识别为模型替换",
			Evidence: map[string]any{
				"model": obs.model, "bucket": obs.bucket, "samples": obs.base.Samples,
			},
		})
	}

	// I9：偏离判定。
	if !obs.ready || !obs.base.Deviates(obs.ratio) {
		return out
	}
	dir, expected := "高", obs.base.Slow
	if obs.ratio < obs.base.Fast {
		dir, expected = "低", obs.base.Fast
	}
	out = append(out, decision.Finding{
		Invariant: "I9",
		Severity:  SeverityViolation,
		Subject:   obs.model + "|" + obs.bucket,
		At:        rec.At,
		Detail: fmt.Sprintf("分词器比例%s于基线：%.4f（期望约 %.4f，区间 [%.4f, %.4f]，%d 样本）",
			dir, obs.ratio, expected, obs.base.Fast, obs.base.Slow, obs.base.Samples),
		Why: "同一模型对同类文本的 token 比例应当稳定；突变说明实际跑的模型被换了",
		Evidence: map[string]any{
			"requestId": rec.RequestID,
			"model":     obs.model,
			"bucket":    obs.bucket,
			"ratio":     obs.ratio,
			"fast":      obs.base.Fast,
			"slow":      obs.base.Slow,
			"samples":   obs.base.Samples,
			"chars":     rec.TokChars,
		},
	})
	return out
}
