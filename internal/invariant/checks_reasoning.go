package invariant

import (
	"fmt"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 阈值：每一项都由真实数据标定，改任何一个都要重跑标定。
//
// 标定方法（2026-11-27，用线上 11-24 与 11-25 两天的决策日志）：
// 把同一套判据跑遍历史，看「命中节点数」与「人工认定的节点」是否一致。
const (
	// minCompletionForThinking：低于此不计可判。
	//
	// reasoning_tokens 计入 completion_tokens（DeepSeek 思考模式），
	// 短输出必然挤压思考 —— 实测按 completion 分桶的思考率：
	//   <60   0.0%（16 条）
	//   60-120 0.0%（14 条）
	//   120-300 18.4%（114 条）
	//   300-800 61.8%（136 条）← 此处分界
	//   800-2K  81.8%（44 条）
	//   >=2K   97.4%（76 条）
	// 拿 300 当界可避开整段「预算不足」区。
	minCompletionForThinking = 300

	// minSilentRun：连续多少条静默才算一个节点。
	//
	// 实测 N=3/4/5/6 在两组样本上的节点数：
	//   N    11-24 坏会话(41 条)   11-25 全量(263 条可判)
	//   3    3                     8
	//   4    3  ← 保留全部人工认定  5  ← 不刷屏
	//   5    2（漏掉段长 4）        4
	//   6    2                     2
	// N=4 是唯一点：坏会话的三处节点（段长 4 / 18 / 7 条）全部保留，
	// 而全量两天只出 5 条。N=3 会多出两段长 2 的瞬时抖动。
	minSilentRun = 4

	// lockedRun：段长到此升级为 violation。
	//
	// 标定：坏会话有一段 18 条、11-25 有一段 15 条；其余段全部 ≤7。
	// 取 8 可把这两段升级，而把「抖动几下就恢复」的段留在 info。
	lockedRun = 8
)

func registerReasoning(r *Registry) {
	r.Add("I12", "声明思考后持续不思考（退化节点）",
		"客户端 tier=low/high/max 要求了思考、输出也足够长，上游却连续多条不给 reasoning —— 用户会直接感知成「突然变笨、废话变多」，而 systemFp 与 tier 全不变，所以 I9/I10/I11 全盲",
		SeverityInfo, checkReasoningSegments)
}

// clockText 把毫秒时间戳渲染成 HH:MM:SS，0 返回 "-"。
//
// 为什么不能直接 formatMillis(ms)[11:]：formatMillis(0) 返回 "-"（长度 1），
// [11:] 会 panic。而 runOne 的 recover 会把 panic 转成 violation 级的
// invariant-panic —— 一条缺 At 的记录就能把断言变成假告警。
// Service.RecordChat 是公开入口，Sink.Submit 也只在 reg.Run **之后**填 At。
func clockText(ms int64) string {
	if ms == 0 {
		return "-"
	}
	t := formatMillis(ms)
	if len(t) < 11 {
		return t
	}
	return t[11:]
}

// checkReasoningSegments 一次观测、按快照产出 I12a / I12b / I12c。
//
// 为什么是一条注册 + 多个 Finding（而不是三条 Check）：
// Registry.Run 对每条记录调用全部已注册 Check，三条各自 Observe 会把段长
// 算成三倍；「只在其中一个里 Observe、其余 return nil」又让结果取决于注册
// 顺序。一个 Check 产出多个 Finding 是 Run 本来就支持的形态，没有这些隐患。
func checkReasoningSegments(rec decision.Record, st *State) []decision.Finding {
	judged, silent := reasoningJudged(rec)
	key := reasoningKey(rec)
	if key == "" || st == nil {
		return nil
	}
	snap, ok := st.ObserveReasoning(key, judged, silent, rec.At)
	if !ok || !judged {
		return nil
	}

	var out []decision.Finding

	if silent {
		// 起点：只在段长刚满阈值时报一次（之后 RunLen 会继续涨，不会重复报）。
		if snap.RunLen == minSilentRun && snap.PriorThink > 0 {
			// 段之前的可判条数 = 历史总量 - 当前段长（当前段也是可判记录）。
			before := snap.PriorJudged - snap.RunLen
			if before < 1 {
				before = 1
			}
			out = append(out, decision.Finding{
				Invariant: "I12a", Severity: SeverityInfo,
				Subject: fmt.Sprintf("%s@%d", key, snap.RunStart), At: rec.At,
				Detail: fmt.Sprintf("此前 %d 条中 %d 条思考（%.0f%%）；自 %s 起连续 %d 条静默",
					before, snap.PriorThink,
					100.0*float64(snap.PriorThink)/float64(before),
					clockText(snap.RunStart), snap.RunLen),
				Evidence: map[string]any{
					"sessionKey": rec.SessionKey, "accountId": rec.AccountID,
					"systemFp": rec.SystemFp, "runStart": snap.RunStart,
					"priorJudged": snap.PriorJudged, "priorThink": snap.PriorThink,
				},
			})
		}
		// 锁死：同样只在段长刚满阈值时报一次。
		if snap.RunLen == lockedRun && snap.PriorThink > 0 {
			out = append(out, decision.Finding{
				Invariant: "I12b", Severity: SeverityViolation,
				Subject: fmt.Sprintf("%s@%d", key, snap.RunStart), At: rec.At,
				Detail: fmt.Sprintf("自 %s 起连续 %d 条实质回合零思考（此前 %d 条中 %d 条思考）",
					clockText(snap.RunStart), snap.RunLen,
					snap.PriorJudged-snap.RunLen, snap.PriorThink),
				Evidence: map[string]any{
					"sessionKey": rec.SessionKey, "systemFp": rec.SystemFp,
					"runStart": snap.RunStart, "runLen": snap.RunLen,
				},
			})
		}
		return out
	}

	// 恢复：本条思考了，且刚被打断的那段够长。
	//
	// 三个条件缺一不可（缺任何一个都会退化成刷屏或假报）：
	//   RecoveryPending   本次是「刚打断」的那一条。不判它，恢复之后的
	//                     每一条思考都会再报一次（4 静默 + 30 思考 ⇒ 30 条）
	//   LastRunLen        刚断的那段够长
	//   PriorThinkBefore  段之前确实思考过。必须用**更新前**的计数：
	//                     PriorThink 已被本条自己 +1，用它当守卫永远为真，
	//                     「开局静默 4 条后首次思考」也会误报「恢复」
	if snap.RecoveryPending && snap.LastRunLen >= minSilentRun && snap.PriorThinkBefore > 0 {
		out = append(out, decision.Finding{
			Invariant: "I12c", Severity: SeverityInfo,
			Subject: fmt.Sprintf("%s@%d", key, snap.LastRunStart), At: rec.At,
			Detail: fmt.Sprintf("静默段结束：%s 起连续 %d 条后恢复思考（本次 reasoning=%d）",
				clockText(snap.LastRunStart), snap.LastRunLen, rec.ReasoningTokens),
			Evidence: map[string]any{
				"sessionKey": rec.SessionKey, "systemFp": rec.SystemFp,
				"lastRunLen": snap.LastRunLen, "requestId": rec.RequestID,
			},
		})
		// 报了才消费：一次退化节点一条 finding。
		st.ConsumeRecovery(key)
	}
	return out
}

// reasoningJudged 报告这条请求是否可判，以及是否静默。
func reasoningJudged(rec decision.Record) (judged, silent bool) {
	if rec.Kind != decision.KindRequest || !rec.Ok || rec.Canceled || rec.TitleRewrite {
		return false, false
	}
	if rec.UsageSource == decision.UsageEstimated {
		return false, false
	}
	switch rec.ReasoningTier {
	case decision.ReasoningTierLow, decision.ReasoningTierHigh, decision.ReasoningTierMax:
	default:
		return false, false // 未声明思考 / 部署前的老记录
	}
	if rec.CompletionTokens < minCompletionForThinking {
		return false, false
	}
	return true, rec.ReasoningTokens <= 1
}

// reasoningKey 是退化段的键。
//
// systemFp 必须入键：opencode 的父会话与其子 agent 共用一个 sessionKey，
// 两者 promptTokens 相差一个数量级（355K vs 125K）。不区分就会让两者
// 交替打断彼此的段 —— 实测那正是 I3b 报出 231 条假「上下文骤降」的成因。
func reasoningKey(rec decision.Record) string {
	if rec.SessionKey == "" || rec.SystemFp == "" {
		return ""
	}
	model := rec.RoutedModel
	if model == "" {
		model = rec.Model
	}
	return SessionKey(rec.SessionKey, rec.AccountID, model) + "|" + rec.SystemFp
}
