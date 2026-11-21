package invariant

import (
	"fmt"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 会话级观测 I3b / 错误分类 I5。
//
// 这两条都不是「违反即证明有 bug」：客户端压缩上下文是合法的，
// 上游冒出新的错误文案也是常态。所以只记 info，用来看趋势与量化覆盖，
// 不用来报警。
func registerSession(r *Registry) {
	r.Add("I3b", "同一会话的上下文骤降（观测）",
		"请求成功、usage 真实、非取消非标题改写，promptTokens 却掉超过 25% —— 记下来供人工判断是压缩还是截断",
		SeverityInfo, checkSessionContextDrop)

	r.Add("I5", "错误必须能被分类",
		"同一错误文案反复出现却始终没有错误码，说明分类器漏了这一类",
		SeverityInfo, checkErrorUnclassified)
}

// minContextForDrop：低于这个量的会话不做比较，避免小请求的抖动被当成骤降。
const minContextForDrop = 50000

// checkSessionContextDrop 比较同一会话相邻两次的 promptTokens。
func checkSessionContextDrop(rec decision.Record, st *State) []decision.Finding {
	if rec.Kind != decision.KindRequest || st == nil || rec.Canceled || rec.TitleRewrite {
		return nil
	}
	// 估算值不可比：它们来自本地 tokenizer，会话之间口径可能不一致。
	if rec.UsageSource == decision.UsageEstimated {
		return nil
	}
	if !rec.Ok || rec.PromptTokens <= 0 {
		return nil
	}
	key := SessionKey(rec.SessionKey, rec.AccountID, rec.Model)
	if key == "" {
		return nil
	}
	prev, ok := st.PrevPrompt(key)
	st.SetPrompt(key, rec.PromptTokens)
	if !ok || prev < minContextForDrop {
		return nil
	}
	// 掉不到 3/4 不算骤降。
	if rec.PromptTokens >= prev*3/4 {
		return nil
	}
	return []decision.Finding{{
		Subject:  key,
		Detail:   fmt.Sprintf("上下文从 %d 降到 %d（-%.0f%%）", prev, rec.PromptTokens, (1-float64(rec.PromptTokens)/float64(prev))*100),
		Evidence: map[string]any{"prev": prev, "current": rec.PromptTokens, "sessionKey": rec.SessionKey, "model": rec.Model},
	}}
}

// errorRepeatThreshold：同一文案出现这么多次仍无错误码，才值得记一条。
// 单次未分类是常态（上游随时冒出新的文案），天天报就是噪音。
const errorRepeatThreshold = 3

// checkErrorUnclassified 统计「失败但无错误码」的文案，达到阈值时记一条。
func checkErrorUnclassified(rec decision.Record, st *State) []decision.Finding {
	if rec.Kind != decision.KindRequest || rec.Ok || rec.Canceled {
		return nil
	}
	if rec.ErrorCode != "" || st == nil {
		return nil
	}
	n := st.BumpError(rec.ErrorText)
	if n != errorRepeatThreshold {
		return nil // 只在恰好达到阈值时报一次，之后不再重复
	}
	return []decision.Finding{{
		Subject:  normalizeErrorText(rec.ErrorText),
		Detail:   fmt.Sprintf("同一错误文案已出现 %d 次仍无错误码", n),
		Evidence: map[string]any{"count": n, "errorText": normalizeErrorText(rec.ErrorText)},
	}}
}
