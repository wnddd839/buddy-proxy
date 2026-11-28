package invariant

import (
	"fmt"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
	"github.com/wnddd839/codebuddy-proxy/internal/tokfp"
)

// I10：同一会话内分词器比例突变。
//
// ## 与 I9 的分工
//
// I9 用**全局**基线：同一 (模型, 分桶) 攒够 3 个样本才判定。
// 好处是能抓「整个模型被换了」；坏处是**只对某个会话**的路由它看不出来 ——
// 全局基线会被大量正常会话稀释。
//
// I10 用**会话级前后对比**：同一会话只需两次即可判定。
// 好处是长会话第一个样本就建起参照；坏处是它抓不到「所有会话同时被换」
// （那种情况 I9 才有效）。
//
// 两者互补，都要。
func registerSessionTok(r *Registry) {
	r.Add("I10", "同一会话内分词器比例不得突变",
		"上游若按会话做路由，全局基线会被其他正常会话稀释而看不出来；会话内前后对比能立刻发现",
		SeverityViolation, checkSessionTokJump)
}

// sessionTokKey 是会话参照的键。
//
// 必须含模型与分桶：换模型或换桶就不是同一个参照系。
// 换模型时比例本来就该变（不同词表），拿它当「突变」是误报。
func sessionTokKey(rec decision.Record) string {
	if rec.SessionKey == "" || rec.TokBucket == "" {
		return ""
	}
	model := rec.RoutedModel
	if model == "" {
		model = rec.Model
	}
	if model == "" {
		return ""
	}
	return rec.SessionKey + "|" + model + "|" + rec.TokBucket
}

// checkSessionTokJump 比较同会话前后两次的比例。
func checkSessionTokJump(rec decision.Record, st *State) []decision.Finding {
	if rec.Kind != decision.KindRequest || !rec.Ok || rec.Canceled {
		return nil
	}
	if rec.TokRatio <= 0 {
		return nil
	}
	// 与 I11 同一理由：分子含额外 token（tools schema / system echo）时
	// 比例虚高，会话内前后对比同样会被污染。
	if rec.HasTools || rec.SystemEchoed {
		return nil
	}
	// tiny / none 桶不参与：模板开销占比过高，比例失真。
	if rec.TokBucket == tokfp.BucketTiny || rec.TokBucket == tokfp.BucketNone {
		return nil
	}
	// 物理不合理值不参与（与 I9 同一闸门）。
	if !tokRatioPlausible(rec.TokRatio) {
		return nil
	}
	key := sessionTokKey(rec)
	if key == "" {
		return nil
	}

	prev, had := st.ObserveSessionRatio(key, rec.TokRatio)
	if !had {
		return nil // 第一次，只建参照
	}
	if prev <= 0 {
		return nil
	}

	// 相对偏差，与 I9 同一容差（8%）。
	delta := rec.TokRatio - prev
	rel := delta / prev
	if rel < 0 {
		rel = -rel
	}
	if rel <= tokTolerance {
		return nil
	}

	return []decision.Finding{{
		Invariant: "I10",
		Severity:  SeverityViolation,
		Subject:   key,
		At:        rec.At,
		Detail: fmt.Sprintf("同一会话内比例从 %.4f 变为 %.4f（%.0f%%，容差 %.0f%%）",
			prev, rec.TokRatio, rel*100, tokTolerance*100),
		Why: "上游若按会话做路由，全局基线会被其他会话稀释；会话内前后对比能立刻发现",
		Evidence: map[string]any{
			"requestId":  rec.RequestID,
			"sessionKey": rec.SessionKey,
			"prev":       prev,
			"current":    rec.TokRatio,
			"bucket":     rec.TokBucket,
			"chars":      rec.TokChars,
		},
	}}
}
