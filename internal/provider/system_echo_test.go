package provider

import (
	"strings"
	"testing"
)

// echo 开关的核心契约：
//  1. system 位**保持不变**（不是搬走，区别于 fold）
//  2. 第一条 user 开头多出完整的 <client_instructions> 包裹副本
//  3. 只注入第一条 user，不逐条复制
//  4. 没有 system 时什么都不做
//  5. 开关关闭时完全不影响原行为
func TestEchoSystemIntoFirstUser(t *testing.T) {
	sys := "You are a helpful coding assistant with tools."
	msgs := []map[string]any{
		{"role": "system", "content": sys},
		{"role": "user", "content": "第一问"},
		{"role": "assistant", "content": "第一答"},
		{"role": "user", "content": "第二问"},
	}
	out := echoSystemIntoFirstUser(msgs)

	if len(out) != len(msgs) {
		t.Fatalf("消息数不应变化：%d → %d", len(msgs), len(out))
	}
	// 1) system 原样保留
	if got := out[0]["content"]; got != sys {
		t.Errorf("system 位被改动：%q", got)
	}
	// 2) 第一条 user 有包裹副本
	first := out[1]["content"].(string)
	if !strings.HasPrefix(first, "<client_instructions>") {
		t.Errorf("第一条 user 没有以包裹开头：%.60q", first)
	}
	if !strings.Contains(first, sys) {
		t.Errorf("第一条 user 的包裹里没有 system 全文")
	}
	if !strings.Contains(first, "</client_instructions>") {
		t.Errorf("包裹没有正确闭合")
	}
	if !strings.HasSuffix(first, "第一问") {
		t.Errorf("原 user 内容应保留在包裹之后：%.60q", first)
	}
	// 3) 第二条 user 不受影响
	if got := out[3]["content"]; got != "第二问" {
		t.Errorf("第二条 user 被误改：%q", got)
	}
	t.Logf("✓ system 保留 + 首条 user 包裹 + 其余不动")
}

// 没有 system 时不应注入任何东西。
func TestEchoNoSystemNoop(t *testing.T) {
	msgs := []map[string]any{{"role": "user", "content": "hi"}}
	out := echoSystemIntoFirstUser(msgs)
	if len(out) != 1 || out[0]["content"] != "hi" {
		t.Errorf("无 system 时应原样返回，实际 %#v", out)
	}
	t.Logf("✓ 无 system 时 no-op")
}

// 多条 system 应合并进同一个包裹（避免多个包裹块）。
func TestEchoMultipleSystemsMerged(t *testing.T) {
	msgs := []map[string]any{
		{"role": "system", "content": "身份A"},
		{"role": "system", "content": "身份B"},
		{"role": "user", "content": "问题"},
	}
	out := echoSystemIntoFirstUser(msgs)
	first := out[2]["content"].(string)
	if strings.Count(first, "<client_instructions>") != 1 {
		t.Errorf("应当只有一个包裹块，实际出现 %d 次", strings.Count(first, "<client_instructions>"))
	}
	if !strings.Contains(first, "身份A") || !strings.Contains(first, "身份B") {
		t.Errorf("两条 system 都应包含")
	}
	t.Logf("✓ 多条 system 合并为一个包裹")
}

// 原切片不能被修改（调用方可能复用）。
func TestEchoDoesNotMutateInput(t *testing.T) {
	msgs := []map[string]any{
		{"role": "system", "content": "sys"},
		{"role": "user", "content": "u"},
	}
	before := msgs[1]["content"]
	_ = echoSystemIntoFirstUser(msgs)
	if msgs[1]["content"] != before {
		t.Errorf("输入切片被就地修改了：%q", msgs[1]["content"])
	}
	t.Logf("✓ 输入未被就地修改")
}

// 没有 user 消息时补一条只有包裹的 user（上游要求至少一条 user）。
func TestEchoNoUserAppendsOne(t *testing.T) {
	msgs := []map[string]any{{"role": "system", "content": "sys"}}
	out := echoSystemIntoFirstUser(msgs)
	if len(out) != 2 {
		t.Fatalf("应补一条 user，实际 %d 条", len(out))
	}
	if out[0]["role"] != "user" {
		t.Errorf("补的 user 应在最前面（system 之后仍需可读），实际 role=%v", out[0]["role"])
	}
	if !strings.Contains(out[0]["content"].(string), "sys") {
		t.Errorf("补的 user 里应含 system 内容")
	}
	t.Logf("✓ 无 user 时补一条包裹 user")
}
