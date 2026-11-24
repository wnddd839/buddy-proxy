package invariant

import (
	"fmt"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 进出对比断言 I3 / I4：只在「网关内部减少了历史」时报。
//
// 为什么不看 promptTokens：客户端压缩、标题改写、取消都会让 token 数
// 合法地掉一大截。唯一能证明「网关自己弄丢了历史」的证据，是送进来的
// 消息条数/字节数与发给上游的之间的差。
func registerIO(r *Registry) {
	r.Add("I3", "网关内部不得减少历史",
		"进出之间的条数差超出策略预期，说明历史在网关里被丢了；客户端发得少不算",
		SeverityViolation, checkHistoryShrunk)

	r.Add("I4", "system 不得无声消失",
		"入站有 system、出站没有，且没有任何策略记录，说明系统提示被静默移除",
		SeverityViolation, checkSystemVanished)
}

// checkHistoryShrunk 检查出站消息条数是否低于「入站 - 策略预期」。
func checkHistoryShrunk(rec decision.Record, _ *State) []decision.Finding {
	if rec.Kind != decision.KindRequest || rec.Canceled {
		return nil
	}
	// 出站闸门：只在出站真的发生过时才判。
	//
	// 为什么不能按 Ok 判（上一版的做法，已撤销）：
	//   出站**之前**失败（消息全被过滤成空、参数校验失败）→ Out* 从未产生，
	//     「出站 0 条」不是丢历史，而是根本没发。这类必须跳过。
	//   出站**之后**失败（上游 5xx / 429 / 流中断）→ Out* 已填充，
	//     那些路径上的「条数变少」是真缺陷，必须继续判。
	//   两种都是 Ok=false，所以 Ok 分不开它们 —— 只有 OutObserved 能。
	//
	// 旧记录兜底：加字段之前的记录没有 OutObserved，但若 OutMessages>0
	// 说明出站显然发生过，照常判。
	if !rec.OutObserved && rec.OutMessages == 0 {
		return nil
	}
	// 没有观测数据（老调用方 / 未接线）时不能判 —— 零值不等于「零条消息」。
	if rec.InMessages <= 0 && rec.OutMessages <= 0 {
		return nil
	}
	expected := 0
	// fold 把 system 搬进 user：条数必然 -1，属预期。
	if rec.SystemForm == "fold" && rec.InSystemLen > 0 {
		expected = -1
	}
	if rec.OutMessages < rec.InMessages+expected {
		return []decision.Finding{{
			Subject: rec.RequestID,
			Detail: fmt.Sprintf("入站 %d 条 → 出站 %d 条（预期最少 %d 条）",
				rec.InMessages, rec.OutMessages, rec.InMessages+expected),
			Evidence: map[string]any{
				"inMessages": rec.InMessages, "outMessages": rec.OutMessages,
				"systemForm": rec.SystemForm, "inSystemLen": rec.InSystemLen,
			},
		}}
	}
	// 条数对但内容全空：同样是丢历史。
	if rec.InBytes > 0 && rec.OutBytes == 0 {
		return []decision.Finding{{
			Subject: rec.RequestID,
			Detail:  fmt.Sprintf("出站消息内容为空，入站有 %d 字符", rec.InBytes),
			Evidence: map[string]any{
				"inBytes": rec.InBytes, "outBytes": rec.OutBytes,
				"inMessages": rec.InMessages, "outMessages": rec.OutMessages,
			},
		}}
	}
	return nil
}

// checkSystemVanished 检查「入站有 system、出站没有」是否被策略解释过。
func checkSystemVanished(rec decision.Record, _ *State) []decision.Finding {
	if rec.Kind != decision.KindRequest || rec.Canceled {
		return nil
	}
	// 与 I3 同一把出站闸门：出站前失败的请求 OutSystemLen 恒为 0，
	// 那不是「system 被移除」，而是「根本没发」。
	// 结构上可达：账号选中后、provider 调用前失败（OAuth 刷新耗尽、
	// body 构建失败）时 InSystemLen 已填而 OutSystemLen 未填。
	if !rec.OutObserved && rec.OutSystemLen == 0 && rec.OutMessages == 0 {
		return nil
	}
	if rec.InSystemLen <= 0 || rec.OutSystemLen > 0 {
		return nil
	}
	// 合法路径：
	//   fold        —— system 搬进 user（条数也 -1，由 I3 一并豁免）
	//   strip system/both —— 定向删除身份句，可能削光
	//   degraded    —— 11128 降级后重放
	//   标题改写     —— 请求形状不同，本来就可能没有 system
	if rec.SystemForm == "fold" || rec.Degraded || rec.TitleRewrite {
		return nil
	}
	if rec.StripScope == "system" || rec.StripScope == "both" {
		return nil
	}
	return []decision.Finding{{
		Subject: rec.RequestID,
		Detail: fmt.Sprintf("入站 system %d 字符，出站 system 为空（form=%q stripScope=%q）",
			rec.InSystemLen, rec.SystemForm, rec.StripScope),
		Evidence: map[string]any{
			"inSystemLen": rec.InSystemLen, "outSystemLen": rec.OutSystemLen,
			"systemForm": rec.SystemForm, "stripScope": rec.StripScope,
			"degraded": rec.Degraded, "titleRewrite": rec.TitleRewrite,
		},
	}}
}
