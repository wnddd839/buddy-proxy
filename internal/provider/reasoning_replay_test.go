package provider

import (
	"testing"
)

// 锁住第一个行为：assistant 上的 reasoning_content 必须原样回到上游 body。
//
// 依据 DeepSeek Thinking Mode 规范（带 tools 时 reasoning_content 必须回传，
// 否则 400）。我们此前一律删除，属于违反规范；LiteLLM 1.100 起也犯过同样的错
// 并被社区报了大量多轮工具调用 bug。
func TestReasoningReplayKeepsAssistantThinking(t *testing.T) {
	const thinking = "用户想知道杭州明天的天气，我需要先拿到日期，再调用天气工具。"
	msgs := []map[string]any{
		{"role": "user", "content": "杭州明天天气怎么样"},
		{
			"role":              "assistant",
			"content":           "我先查一下日期。",
			"reasoning_content": thinking,
			"tool_calls": []any{
				map[string]any{
					"id": "call_1", "type": "function",
					"function": map[string]any{"name": "get_date", "arguments": "{}"},
				},
			},
		},
		{"role": "tool", "tool_call_id": "call_1", "content": "2026-11-20"},
	}

	out := EnsureUpstreamMessagesWithReplay(msgs, true, nil, nil, true)
	var found bool
	for _, m := range out {
		if m["role"] != "assistant" {
			continue
		}
		got, ok := m["reasoning_content"]
		if !ok {
			t.Fatal("assistant 的 reasoning_content 被丢掉了")
		}
		if got != thinking {
			t.Fatalf("reasoning_content 被改写：%q", got)
		}
		found = true
	}
	if !found {
		t.Fatal("没找到 assistant 消息")
	}
}

// 关掉回传时必须真的不回传 —— 这是出问题时的回滚开关。
func TestReasoningReplayCanBeDisabled(t *testing.T) {
	msgs := []map[string]any{
		{
			"role":              "assistant",
			"content":           "hi",
			"reasoning_content": "some thinking",
		},
	}
	out := EnsureUpstreamMessagesWithReplay(msgs, true, nil, nil, false)
	for _, m := range out {
		if _, ok := m["reasoning_content"]; ok {
			t.Fatal("关闭后仍然回传了 reasoning_content")
		}
	}
}

// 只认 reasoning_content 这一个字段名：OpenRouter 的 reasoning /
// reasoning_details 是不同形态，混进来会把命名空间搞乱。
func TestReasoningReplayIgnoresOtherFieldNames(t *testing.T) {
	msgs := []map[string]any{
		{
			"role":           "assistant",
			"content":        "hi",
			"reasoning":      "plain reasoning",
			"reasoning_text": "another",
		},
	}
	out := EnsureUpstreamMessagesWithReplay(msgs, true, nil, nil, true)
	for _, m := range out {
		if _, ok := m["reasoning"]; ok {
			t.Fatal("不该注入 reasoning 字段")
		}
		if _, ok := m["reasoning_text"]; ok {
			t.Fatal("不该注入 reasoning_text 字段")
		}
		if _, ok := m["reasoning_content"]; ok {
			t.Fatal("源消息没有 reasoning_content，不该凭空造一个")
		}
	}
}

// 非 assistant 角色带 reasoning_content 不应被注入（上游只认 assistant 的）。
func TestReasoningReplayOnlyOnAssistant(t *testing.T) {
	msgs := []map[string]any{
		{"role": "user", "content": "hi", "reasoning_content": "should not appear"},
		{"role": "system", "content": "sys", "reasoning_content": "should not appear"},
	}
	out := EnsureUpstreamMessagesWithReplay(msgs, true, nil, nil, true)
	for _, m := range out {
		if _, ok := m["reasoning_content"]; ok {
			t.Fatalf("角色 %v 不该带 reasoning_content", m["role"])
		}
	}
}

// 空串不注入：上游对空 reasoning_content 的行为未测，不制造噪声。
func TestReasoningReplaySkipsEmptyThinking(t *testing.T) {
	msgs := []map[string]any{
		{"role": "assistant", "content": "hi", "reasoning_content": ""},
	}
	out := EnsureUpstreamMessagesWithReplay(msgs, true, nil, nil, true)
	for _, m := range out {
		if _, ok := m["reasoning_content"]; ok {
			t.Fatal("空 reasoning_content 不该被注入")
		}
	}
}

// 折叠模式（keepSystem=false）下同样要回传：折叠只动 system，
// 历史 thinking 与 system 策略无关。
func TestReasoningReplayInFoldMode(t *testing.T) {
	msgs := []map[string]any{
		{"role": "system", "content": "you are a helpful assistant"},
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "hello", "reasoning_content": "thinking about greeting"},
	}
	out := EnsureUpstreamMessagesWithReplay(msgs, false, nil, nil, true)
	var sawThinking bool
	for _, m := range out {
		if m["reasoning_content"] == "thinking about greeting" {
			sawThinking = true
		}
	}
	if !sawThinking {
		t.Fatal("折叠模式下历史 thinking 也应回传")
	}
}
