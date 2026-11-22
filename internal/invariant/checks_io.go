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
	// 失败请求不判：很多失败发生在「出站之前」（参数校验失败、
	// 消息全被过滤成空、上游连接都没建立）。那些路径上出站根本没发生，
	// 「出站 0 条」不是「历史被丢了」，而是「根本没发」。
	//
	// 实测证据（2026-11-22）：一条带 tools 的请求因
	// "no valid messages after normalization" 在出站前就返回错误，
	// 但 recording 里 outMessages=0，I3 把它误判成「入站 1 条 → 出站 0 条」。
	//
	// 失败请求的历史处理由 I4（system 消失）与错误分类负责，不归 I3。
	if rec.Kind != decision.KindRequest || rec.Canceled || !rec.Ok {
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
