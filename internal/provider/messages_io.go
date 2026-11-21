package provider

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
	// InSystemLen / OutSystemLen 是 system 角色消息的字节数之和。
	// I4（system 无声消失）只看它：条数不变但 system 字节归零，同样是丢失。
	InSystemLen  int `json:"inSystemLen,omitempty"`
	OutSystemLen int `json:"outSystemLen,omitempty"`
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
