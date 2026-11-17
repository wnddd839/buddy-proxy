package provider

import (
	"reflect"
	"testing"
)

func TestKeepSystemPreservesRoleAndPosition(t *testing.T) {
	in := []map[string]any{
		{"role": "system", "content": "You are Claude Code, Anthropic's official CLI for Claude."},
		{"role": "user", "content": "hello"},
	}
	out := EnsureUpstreamMessagesWith(in, true, nil, nil)
	if len(out) != 2 {
		t.Fatalf("条数变了：%d", len(out))
	}
	if out[0]["role"] != "system" {
		t.Fatalf("首条应仍是 system，实际 %v", out[0]["role"])
	}
	if out[0]["content"] != in[0]["content"] {
		t.Fatalf("system 内容被改动：%v", out[0]["content"])
	}
	if out[1]["content"] != "hello" {
		t.Fatalf("user 被改动：%v", out[1]["content"])
	}
}

func TestKeepSystemDoesNotWrapInClientInstructions(t *testing.T) {
	in := []map[string]any{
		{"role": "system", "content": "SYSTEM-TEXT"},
		{"role": "user", "content": "hi"},
	}
	out := EnsureUpstreamMessagesWith(in, true, nil, nil)
	for _, m := range out {
		if s, ok := m["content"].(string); ok {
			if s == wrapClientInstructions("SYSTEM-TEXT") {
				t.Fatal("keep 模式不应把 system 折叠成 <client_instructions>")
			}
		}
	}
	if out[0]["content"] != "SYSTEM-TEXT" {
		t.Fatalf("system 原文应原样保留，实际 %v", out[0]["content"])
	}
}

func TestFoldIsDefaultPreserved(t *testing.T) {
	in := []map[string]any{
		{"role": "system", "content": "You are opencode."},
		{"role": "user", "content": "hi"},
	}
	// 旧签名必须与历史行为一致
	viaOld := EnsureUpstreamMessages(in)
	viaNew := EnsureUpstreamMessagesWith(in, false, nil, nil)
	if !reflect.DeepEqual(viaOld, viaNew) {
		t.Fatalf("旧签名与 keepSystem=false 应等价\nold=%v\nnew=%v", viaOld, viaNew)
	}
	if viaOld[0]["content"] != canonicalUpstreamSystem {
		t.Fatalf("fold 应替换为 canonical，实际 %v", viaOld[0]["content"])
	}
}

func TestKeepSystemStillMergesToolAssistants(t *testing.T) {
	in := []map[string]any{
		{"role": "system", "content": "S"},
		{"role": "assistant", "content": nil, "tool_calls": []any{
			map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "bash", "arguments": "{}"}}}},
		{"role": "assistant", "content": nil, "tool_calls": []any{
			map[string]any{"id": "c2", "type": "function", "function": map[string]any{"name": "bash", "arguments": "{}"}}}},
	}
	out := EnsureUpstreamMessagesWith(in, true, nil, nil)
	merged := 0
	for _, m := range out {
		if m["role"] == "assistant" {
			merged++
			if n := len(asAnySlice(m["tool_calls"])); n != 2 {
				t.Fatalf("keep 模式下空 tool assistant 应被合并，tool_calls=%d", n)
			}
		}
	}
	if merged != 1 {
		t.Fatalf("应只剩 1 条 assistant，实际 %d", merged)
	}
}

func TestKeepSystemStillDropsEmptyMessages(t *testing.T) {
	in := []map[string]any{
		{"role": "system", "content": "S"},
		{"role": "user", "content": "   "},
		{"role": "user", "content": "real"},
	}
	out := EnsureUpstreamMessagesWith(in, true, nil, nil)
	users := 0
	for _, m := range out {
		if m["role"] == "user" {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("空 user 应被丢弃，实际剩 %d 条 user", users)
	}
}

func TestKeepSystemStillNormalizesToolOutput(t *testing.T) {
	// 用结构化数组输入证明 NormalizeToolOutput 真的被调用：
	// 不经它时 content 仍是 []any，经它才被拍平成 "ab"。
	in := []map[string]any{
		{"role": "system", "content": "S"},
		{"role": "assistant", "content": "ok", "tool_calls": []any{
			map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "bash", "arguments": "{}"}}}},
		{"role": "tool", "tool_call_id": "c1", "content": []any{
			map[string]any{"type": "text", "text": "a"},
			map[string]any{"type": "text", "text": "b"},
		}},
	}
	out := EnsureUpstreamMessagesWith(in, true, nil, nil)
	found := false
	for _, m := range out {
		if m["role"] != "tool" {
			continue
		}
		found = true
		got, ok := m["content"].(string)
		if !ok {
			t.Fatalf("tool 输出应被 NormalizeToolOutput 拍平成字符串，实际 %T", m["content"])
		}
		if got != "ab" {
			t.Fatalf("拍平结果应为 %q，实际 %q", "ab", got)
		}
	}
	if !found {
		t.Fatal("tool 消息不应被丢弃")
	}
}

func TestKeepSystemMapsDeveloperToSystem(t *testing.T) {
	in := []map[string]any{
		{"role": "developer", "content": "DEV-TEXT"},
		{"role": "user", "content": "hi"},
	}
	out := EnsureUpstreamMessagesWith(in, true, nil, nil)
	if out[0]["role"] != "system" {
		t.Fatalf("developer 应归一为 system，实际 %v", out[0]["role"])
	}
	if out[0]["content"] != "DEV-TEXT" {
		t.Fatalf("developer 内容应原样保留，实际 %v", out[0]["content"])
	}
}

func TestKeepSystemGlobalAddsMissingSystem(t *testing.T) {
	// provider 层不负责 global 兜底（在 Complete 里），此测试锁「无 system 时不伪造」
	in := []map[string]any{{"role": "user", "content": "hi"}}
	out := EnsureUpstreamMessagesWith(in, true, nil, nil)
	for _, m := range out {
		if m["role"] == "system" {
			t.Fatal("客户端没发 system 时 EnsureUpstreamMessagesWith 不应伪造")
		}
	}
}

func TestKeepSystemMultipleSystemsAllPreserved(t *testing.T) {
	in := []map[string]any{
		{"role": "system", "content": "S1"},
		{"role": "system", "content": "S2"},
		{"role": "user", "content": "hi"},
	}
	out := EnsureUpstreamMessagesWith(in, true, nil, nil)
	if len(out) != 3 {
		t.Fatalf("应保留 3 条，实际 %d", len(out))
	}
	if out[0]["content"] != "S1" || out[1]["content"] != "S2" {
		t.Fatalf("多条 system 应按原顺序保留，实际 %v / %v", out[0]["content"], out[1]["content"])
	}
}
