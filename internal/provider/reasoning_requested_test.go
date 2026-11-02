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
		{"object overrides effort", provider.ChatOptions{ReasoningEffort: "high", Reasoning: map[string]any{"effort": "none"}}, false},
		{"empty object overrides effort", provider.ChatOptions{ReasoningEffort: "high", Reasoning: map[string]any{}}, false},
		{"enabled object", provider.ChatOptions{ReasoningEffort: "none", Reasoning: map[string]any{"effort": "high"}}, true},
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
