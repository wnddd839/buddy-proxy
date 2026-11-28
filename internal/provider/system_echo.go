package provider

import (
	"fmt"
	"strings"
)

// echoSystemIntoFirstUser 把所有 system 文本复制一份到第一条 user 的开头，
// 用 <client_instructions> 完整包裹，system 本身保持不变。
//
// 与 foldClientInstructions 的区别：
//   - fold：把 system **搬走**（首条换成 canonical，客户端原文进 user）
//   - echo：把 system **复制**一份（system 位原样保留，user 里多一份）
//
// 只注入第一条 user：把同样的长文本复制到每个 user 会成倍放大 token，
// 而首条 user 已在最前面，模型注意力覆盖得到。
func echoSystemIntoFirstUser(messages []map[string]any) []map[string]any {
	var parts []string
	for _, m := range messages {
		if NormalizeMessageRole(fmt.Sprint(m["role"])) != "system" {
			continue
		}
		text := strings.TrimSpace(SystemDigestText(m["content"]))
		if text == "" || text == "<nil>" {
			continue
		}
		parts = append(parts, text)
	}
	if len(parts) == 0 {
		return messages
	}
	wrapped := wrapClientInstructions(strings.Join(parts, "\n\n"))

	for i, m := range messages {
		if NormalizeMessageRole(fmt.Sprint(m["role"])) != "user" {
			continue
		}
		out := make([]map[string]any, len(messages))
		copy(out, messages)
		switch content := m["content"].(type) {
		case string:
			out[i] = map[string]any{"role": "user", "content": wrapped + "\n\n" + content}
		case nil:
			out[i] = map[string]any{"role": "user", "content": wrapped}
		default:
			// 结构化 content（含图片等）：包裹块放最前，其余原样保留。
			blocks := []any{map[string]any{"type": "text", "text": wrapped}}
			if contentParts := asAnySlice(content); contentParts != nil {
				blocks = append(blocks, contentParts...)
			} else {
				blocks = append(blocks, content)
			}
			out[i] = map[string]any{"role": "user", "content": blocks}
		}
		return out
	}
	return append([]map[string]any{{"role": "user", "content": wrapped}}, messages...)
}
