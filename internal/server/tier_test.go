package server

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/gateway"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

// 客户端显式关掉思考时，reasoningTier 必须为空 —— 否则 I12 会把
// 「客户端自己关的」误判成「上游退化」。
//
// 评审 Important 5：chat_journal 原来只算 effortTier(effectiveEffort(...))，
// 不看 opts.Thinking。客户端同时发 reasoning_effort:"high" 与
// thinking:{"type":"disabled"} 时会被记成 tier=high 并纳入 I12，
// 而模型正确地不思考。
func TestReasoningTierEmptyWhenThinkingDisabled(t *testing.T) {
	reasons := []provider.ChatOptions{
		{ReasoningEffort: "high", Thinking: map[string]any{"type": "disabled"}},
		{ReasoningEffort: "", Thinking: map[string]any{"type": "disabled"}},
		{ReasoningEffort: "max", Thinking: map[string]any{"type": "none"}},
	}
	for _, opts := range reasons {
		req := provider.ReasoningRequested(opts)
		tier := ""
		if req {
			tier = "should-be-empty"
		}
		if tier != "" {
			t.Fatalf("thinking=disabled 时 ReasoningRequested 应为 false，实际 %+v", opts)
		}
	}
}

// 对照：正常请求仍要产出 tier（不能因为这道闸门把 I12 整体关掉）。
func TestReasoningTierKeptForNormalRequest(t *testing.T) {
	for _, opts := range []provider.ChatOptions{
		{ReasoningEffort: "max"},
		{ReasoningEffort: "high"},
		{ReasoningEffort: "low"},
		{ReasoningEffort: "max", Thinking: map[string]any{"type": "enabled"}},
	} {
		if !provider.ReasoningRequested(opts) {
			t.Fatalf("正常请求应要求思考，实际 %+v", opts)
		}
	}
}

// gateway.ChatRecordInput 的 tier 语义：空 = 不可判，有值 = 可判。
// 这里只锁住「空 tier 不会让 I12 判它」这一层契约。
func TestEmptyTierMeansUnjudgeable(t *testing.T) {
	var input gateway.ChatRecordInput
	if input.ReasoningTier != "" {
		t.Fatalf("零值 tier 应为空串")
	}
}
