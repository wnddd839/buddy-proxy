package provider

import "testing"

// 国际站（www.codebuddy.ai）要求首条消息必须是 system，否则 400 / 11128。
// 国内站没有这个限制，所以补全逻辑必须只作用在 global 上。
func TestEnsureSystemFirstAddsWhenMissing(t *testing.T) {
	msgs := []map[string]any{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "hello"},
	}
	got := EnsureSystemFirst(msgs)
	if len(got) != len(msgs)+1 {
		t.Fatalf("want %d messages, got %d", len(msgs)+1, len(got))
	}
	if role := got[0]["role"]; role != "system" {
		t.Fatalf("first role = %v, want system", role)
	}
	if got[0]["content"] != canonicalUpstreamSystem {
		t.Fatalf("system content = %v, want canonical", got[0]["content"])
	}
	// 原消息必须原样保留在后面。
	if got[1]["content"] != "hi" || got[2]["content"] != "hello" {
		t.Fatalf("payload not preserved: %v", got)
	}
}

func TestEnsureSystemFirstKeepsExisting(t *testing.T) {
	msgs := []map[string]any{
		{"role": "system", "content": "custom instruction"},
		{"role": "user", "content": "hi"},
	}
	got := EnsureSystemFirst(msgs)
	if len(got) != 2 {
		t.Fatalf("want 2 messages, got %d", len(got))
	}
	if got[0]["content"] != "custom instruction" {
		t.Fatalf("existing system replaced: %v", got[0]["content"])
	}
}

func TestEnsureSystemFirstEmpty(t *testing.T) {
	if got := EnsureSystemFirst(nil); len(got) != 0 {
		t.Fatalf("nil should stay empty, got %v", got)
	}
}

// EnsureUpstreamMessages 在没有 system 时不补 system —— 这正是要加
// EnsureSystemFirst 的原因，防止以后有人误以为前者已经兜住。
func TestEnsureUpstreamMessagesDoesNotAddSystem(t *testing.T) {
	msgs := []map[string]any{{"role": "user", "content": "hi"}}
	got := EnsureUpstreamMessages(msgs)
	if len(got) == 0 {
		t.Fatal("unexpected empty result")
	}
	if got[0]["role"] == "system" {
		t.Fatal("EnsureUpstreamMessages unexpectedly added a system message")
	}
}
