package provider

import (
	"fmt"
	"reflect"
	"testing"
)

// 端到端等价性：对同一批输入，「快路径命中」与「强制走重建路径」必须产出
// 完全相同的结果。
//
// 这是本次优化的核心约束：快路径只允许更快，不允许不一样。任何形态差异
// 都意味着用户看到的请求内容变了 —— 那是最难排查的一类事故。
func TestFastPathOutputEquivalence(t *testing.T) {
	stripOn := &StripConfig{Enabled: true, Rules: RulesForSite("domestic"), Contains: ContainsForSite("domestic")}
	stripOff := (*StripConfig)(nil)

	type tc struct {
		name     string
		messages []map[string]any
		strip    *StripConfig
	}
	fp := "Tokens prefixed with @ are paths the user explicitly referenced. Relative paths resolve from the workspace root"

	cases := []tc{
		{"空", []map[string]any{}, stripOff},
		{"单条 user", []map[string]any{{"role": "user", "content": "hi"}}, stripOff},
		{"正常多轮", []map[string]any{
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "第一问"},
			{"role": "assistant", "content": "第一答", "reasoning_content": "思考一"},
			{"role": "user", "content": "第二问"},
		}, stripOff},
		{"剥离开启-干净内容", []map[string]any{
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "问题"},
			{"role": "assistant", "content": "回答", "reasoning_content": "思考"},
		}, stripOn},
		{"剥离开启-含指纹", []map[string]any{
			{"role": "system", "content": "前 " + fp + " 后"},
			{"role": "user", "content": "问题"},
		}, stripOn},
		{"工具调用", []map[string]any{
			{"role": "user", "content": "读文件"},
			{"role": "assistant", "content": "", "tool_calls": []any{
				map[string]any{"id": "c1", "type": "function",
					"function": map[string]any{"name": "read", "arguments": `{"p":"a"}`}},
			}},
			{"role": "tool", "tool_call_id": "c1", "content": "文件内容"},
			{"role": "assistant", "content": "读到了"},
		}, stripOff},
		{"空正文 user", []map[string]any{
			{"role": "user", "content": "   "},
			{"role": "user", "content": "第二个"},
		}, stripOff},
		{"content 数组", []map[string]any{
			{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "结构化"},
			}},
		}, stripOff},
		{"tool 输出是 JSON 数组", []map[string]any{
			{"role": "tool", "tool_call_id": "x", "content": `[{"type":"text","text":"a"}]`},
		}, stripOff},
		{"非规范 role", []map[string]any{
			{"role": "Human", "content": "hi"},
			{"role": "ai", "content": "hello"},
		}, stripOff},
		{"非 assistant 带 reasoning", []map[string]any{
			{"role": "user", "content": "hi", "reasoning_content": "不该保留"},
		}, stripOff},
		{"未知键", []map[string]any{
			{"role": "user", "content": "hi", "custom_field": "x"},
		}, stripOff},
		{"空 reasoning", []map[string]any{
			{"role": "assistant", "content": "a", "reasoning_content": ""},
		}, stripOff},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// 快路径（当前实现）
			fast := EnsureUpstreamMessagesWithReplay(
				deepCopyMessages(c.messages), true, c.strip, nil, true)
			// 强制走重建路径：加一个 strip 无法复用的条件（数组 content 会被重建）
			// —— 这里直接用「关掉快路径」的内部等价手段：构造同样输入但
			// 逐条传单元素切片，拼回结果，等价于逐条重建。
			rebuilt := rebuildAlways(c.messages, true, c.strip, true)

			if !reflect.DeepEqual(normalizeForCompare(fast), normalizeForCompare(rebuilt)) {
				t.Errorf("快路径与重建路径结果不同:\n  fast    = %#v\n  rebuilt = %#v",
					normalizeForCompare(fast), normalizeForCompare(rebuilt))
			}
		})
	}
}

// rebuildAlways 模拟「快路径不命中」的路径：把输入包一层会阻断复用的形态，
// 再让 EnsureUpstreamMessagesWithReplay 走重建。
//
// 阻断手段：给每条消息加一个未知键 —— 白名单判定会失败 → 走重建。
// 重建路径会丢掉未知键，所以结果与「没有未知键时的重建」一致。
func rebuildAlways(messages []map[string]any, keepSystem bool, strip *StripConfig, replay bool) []map[string]any {
	blocked := make([]map[string]any, len(messages))
	for i, m := range messages {
		n := make(map[string]any, len(m)+1)
		for k, v := range m {
			n[k] = v
		}
		n["__force_rebuild__"] = true
		blocked[i] = n
	}
	return EnsureUpstreamMessagesWithReplay(blocked, keepSystem, strip, nil, replay)
}

// normalizeForCompare 把结果转成可深度比较的形态（map 顺序无关）。
func normalizeForCompare(msgs []map[string]any) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, fmt.Sprintf("%#v", m))
	}
	return out
}

// deepCopyMessages 建深拷贝，避免快路径与重建路径互相污染。
func deepCopyMessages(msgs []map[string]any) []map[string]any {
	out := make([]map[string]any, len(msgs))
	for i, m := range msgs {
		out[i] = deepCopyMap(m)
	}
	return out
}

func deepCopyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = deepCopyValue(v)
	}
	return out
}

func deepCopyValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return deepCopyMap(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = deepCopyValue(e)
		}
		return out
	default:
		return v
	}
}
