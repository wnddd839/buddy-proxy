package provider

import "github.com/wnddd839/codebuddy-proxy/internal/tokfp"

// MessagesIO 是一次请求「进」「出」两侧的消息统计。
//
// 存在的理由：判断「网关有没有把历史弄丢」不能看 token 数 —— 客户端
// 自己压缩上下文、发标题改写请求都会让 promptTokens 合法地掉一大截。
// 唯一能证明「网关内部减少了历史」的证据，是**送进来的消息条数与字节数**
// 与**发给上游的消息条数与字节数**之间的差。
//
// 只统计条数与字节数，不存内容，也不做任何语义判断。
type MessagesIO struct {
	InMessages  int `json:"inMessages,omitempty"`
	OutMessages int `json:"outMessages,omitempty"`
	InBytes     int `json:"inBytes,omitempty"`
	OutBytes    int `json:"outBytes,omitempty"`
	// InSystemLen / OutSystemLen 是 system 角色消息的 **UTF-8 字节数**之和
	// （Go 的 len(string) 就是字节数）。
	//
	// 注意与 tokfp.Composition.Chars 区分：那边按 unicode 字符数计
	// （RuneCountInString），因为 token/字符比要与分词器统计同口径。
	// 这里用字节数与 StripReport.Bytes 对齐，便于「剥了多少」直接相减。
	//
	// I4（system 无声消失）只看它：条数不变但 system 归零，同样是丢失。
	InSystemLen  int `json:"inSystemLen,omitempty"`
	OutSystemLen int `json:"outSystemLen,omitempty"`
	// Composition 是入站内容的字符成分（中文/数字/ASCII 分开计）。
	//
	// 为什么要分类：不同类别的 token 密度差 2~3 倍（ASCII≈0.22、
	// 中文≈0.64、数字≈0.46~0.60）。混在一起算比例会互相抵消，
	// 把真正的分词器差异淹没。
	Composition tokfp.Composition `json:"composition,omitempty"`
	// PromptTokens 是上游返回的 prompt_tokens（0 = 未观测）。
	PromptTokens int `json:"promptTokens,omitempty"`
	// OutObserved 表示出站侧真的被统计过（= 请求确实发往上游）。
	//
	// 为什么需要它：出站前失败的请求（消息全被过滤成空、参数校验失败）
	// 从未构造过出站 payload，此时 OutMessages=0 不代表「发出去了但丢光了」，
	// 而是「根本没发」。I3/I4 必须靠这个标志区分两者。
	OutObserved bool `json:"outObserved,omitempty"`
}

// FillMessagesIO 把 in / out 两侧的统计写进 dst（dst 为 nil 时是空操作）。
//
// 字节数统计 content 的字符长度（不是 UTF-8 字节数）：它的用途是「有没有
// 大幅变少」，与编码无关；用 len(string) 会让中文占比不同的两个请求
// 在同一逻辑长度下字节数差两倍，反而难比较。
func FillMessagesIO(dst *MessagesIO, in, out []map[string]any) {
	if dst == nil {
		return
	}
	// nil 表示「这一侧本次不统计」，而不是「这一侧是空的」。
	// 网关入口先用 nil 的 out 预填入站侧，随后 provider 再填出站侧。
	if in != nil {
		dst.InMessages, dst.InBytes, dst.InSystemLen = countMessages(in)
		dst.Composition = tokfp.Of(in)
	}
	if out != nil {
		dst.OutMessages, dst.OutBytes, dst.OutSystemLen = countMessages(out)
	}
}

// countMessages 返回条数、内容总长度、system 内容总长度。
func countMessages(msgs []map[string]any) (count, total, system int) {
	count = len(msgs)
	for _, m := range msgs {
		n := contentLen(m["content"])
		total += n
		if NormalizeMessageRole(toString(m["role"])) == "system" {
			system += n
		}
	}
	return count, total, system
}

// contentLen 统计一条消息 content 的字符数。
// content 既可能是字符串，也可能是 [{type:"text", text:"..."}] 数组。
func contentLen(v any) int {
	switch c := v.(type) {
	case string:
		return len(c)
	case []any:
		total := 0
		for _, part := range c {
			m, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if s, ok := m["text"].(string); ok {
				total += len(s)
			}
		}
		return total
	case nil:
		return 0
	default:
		// 其它形态（数字/对象）不参与统计：它们不是历史正文。
		return 0
	}
}

// HasSystemRole 报告消息里是否真的有 system（供 I4 的入站侧判定复用）。
func HasSystemRole(msgs []map[string]any) bool {
	for _, m := range msgs {
		if NormalizeMessageRole(toString(m["role"])) == "system" {
			return true
		}
	}
	return false
}
