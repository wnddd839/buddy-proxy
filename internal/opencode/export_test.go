package opencode_test

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/models"
	"github.com/wnddd839/codebuddy-proxy/internal/opencode"
)

func TestVariantsForGLMFlash(t *testing.T) {
	variants := opencode.VariantsForModel(models.Model{
		ID:                "glm-5.3-flash",
		SupportsReasoning: true,
		Reasoning: map[string]any{
			"supportedEfforts":   []any{"low", "high", "max"},
			"canDisableThinking": true,
		},
	})
	if variants["low"]["reasoningEffort"] != "low" {
		t.Fatalf("low=%v", variants["low"])
	}
	if variants["max"]["reasoningEffort"] != "max" {
		t.Fatalf("max=%v", variants["max"])
	}
	if variants["none"]["thinking"] == nil {
		t.Fatalf("expected none thinking disable, got %v", variants["none"])
	}
}

func TestModelListFields(t *testing.T) {
	fields := opencode.ModelListFields(models.Model{
		ID:                "glm-5.3-flash",
		SupportsReasoning: true,
		MaxInputTokens:    1_000_000,
		MaxOutputTokens:   32_000,
		MaxAllowedSize:    1_000_000,
		Reasoning: map[string]any{
			"supportedEfforts": []any{"low", "high"},
			"defaultEffort":    "high",
		},
	})
	if fields["reasoning"] != true {
		t.Fatalf("reasoning=%v want true", fields["reasoning"])
	}
	cfg, ok := fields["reasoning_config"].(map[string]any)
	if !ok || cfg["defaultEffort"] != "high" {
		t.Fatalf("reasoning_config=%v", fields["reasoning_config"])
	}
	if fields["interleaved"] == nil {
		t.Fatalf("missing interleaved")
	}
	variants, ok := fields["variants"].(map[string]map[string]any)
	if !ok || variants["high"]["reasoningEffort"] != "high" {
		t.Fatalf("variants=%v", fields["variants"])
	}
	if fields["context_length"] != 1_000_000 || fields["max_input_tokens"] != 1_000_000 {
		t.Fatalf("input limits=%v", fields)
	}
	if fields["max_output_tokens"] != 32_000 {
		t.Fatalf("max_output_tokens=%v", fields["max_output_tokens"])
	}
	limit, ok := fields["limit"].(map[string]any)
	if !ok || limit["context"] != 1_000_000 || limit["output"] != 32_000 {
		t.Fatalf("limit=%v", fields["limit"])
	}
}

func TestLiteLLMModelInfoEntry(t *testing.T) {
	entry := opencode.LiteLLMModelInfoEntry(models.Model{
		ID:                "glm-5.3-flash",
		SupportsReasoning: true,
		MaxInputTokens:    1_000_000,
		MaxOutputTokens:   32_000,
		Reasoning: map[string]any{
			"supportedEfforts":   []any{"low", "high", "max"},
			"canDisableThinking": true,
		},
	})
	info := entry["model_info"].(map[string]any)
	if info["supports_reasoning"] != true {
		t.Fatalf("supports_reasoning=%v", info["supports_reasoning"])
	}
	if info["supports_max_reasoning_effort"] != true {
		t.Fatalf("supports_max_reasoning_effort=%v", info["supports_max_reasoning_effort"])
	}
	if info["supports_none_reasoning_effort"] != true {
		t.Fatalf("supports_none_reasoning_effort=%v", info["supports_none_reasoning_effort"])
	}
	if info["max_input_tokens"] != 1_000_000 {
		t.Fatalf("max_input_tokens=%v", info["max_input_tokens"])
	}
	if info["max_output_tokens"] != 32_000 || info["max_tokens"] != 32_000 {
		t.Fatalf("output limits=%v", info)
	}
}
