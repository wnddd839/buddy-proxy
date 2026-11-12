package provider_test

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

func TestReasoningRequestedRespectsExplicitDisableAndPrecedence(t *testing.T) {
	tests := []struct {
		name string
		opts provider.ChatOptions
		want bool
	}{
		{"unset", provider.ChatOptions{}, false},
		{"effort", provider.ChatOptions{ReasoningEffort: "high"}, true},
		{"none", provider.ChatOptions{ReasoningEffort: "none"}, false},
		{"disabled thinking", provider.ChatOptions{Thinking: map[string]any{"type": "disabled"}}, false},
		{"enabled thinking", provider.ChatOptions{Thinking: map[string]any{"type": "enabled"}}, true},
		// 对象里的 effort=none 明确表示关闭 ⇒ false（这条语义没变）。
		{"object disables", provider.ChatOptions{Reasoning: map[string]any{"effort": "none"}}, false},
		// 空对象本身不代表要求思考 —— 它不会让上游思考（实测 thinking=0）。
		{"empty object alone", provider.ChatOptions{Reasoning: map[string]any{}}, false},
		// 对象带 effort=high 时会被提升成扁平字段 ⇒ 真正要求思考。
		{"object effort lifted", provider.ChatOptions{Reasoning: map[string]any{"effort": "high"}}, true},
		{"explicit thinking disable", provider.ChatOptions{ReasoningEffort: "high", Thinking: map[string]any{"type": "disabled"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := provider.ReasoningRequested(tt.opts); got != tt.want {
				t.Fatalf("ReasoningRequested=%v want %v", got, tt.want)
			}
		})
	}
}
