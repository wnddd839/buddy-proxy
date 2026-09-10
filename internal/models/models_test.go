package models

import (
	"encoding/json"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

func TestMergeModelsByID(t *testing.T) {
	merged := mergeModelsByID(
		[]map[string]any{{"id": "gpt-5.4"}, {"id": "gemini-3.5-flash"}},
		[]map[string]any{{"id": "hy4-preview"}, {"id": "gpt-5.4"}},
	)
	if len(merged) != 3 {
		t.Fatalf("len=%d want 3: %+v", len(merged), merged)
	}
	ids := make([]string, len(merged))
	for i, m := range merged {
		ids[i] = modelRowID(m)
	}
	if ids[0] != "gpt-5.4" || ids[1] != "gemini-3.5-flash" || ids[2] != "hy4-preview" {
		t.Fatalf("order/ids=%v", ids)
	}
}

func TestV3ConfigCandidateBasesGlobalMergesCopilot(t *testing.T) {
	bases := v3ConfigCandidateBases(provider.ChatOptions{
		Site:    "global",
		BaseURL: "https://www.codebuddy.ai",
	})
	if len(bases) != 2 || bases[0] != "https://www.codebuddy.ai" || bases[1] != "https://copilot.tencent.com" {
		t.Fatalf("global bases=%v", bases)
	}
	domestic := v3ConfigCandidateBases(provider.ChatOptions{Site: "domestic"})
	if len(domestic) != 1 || domestic[0] != "https://copilot.tencent.com" {
		t.Fatalf("domestic bases=%v", domestic)
	}
}

func TestToAdminModelsPreservesContextLimits(t *testing.T) {
	out := ToAdminModels([]map[string]any{
		{
			"id":              "deepseek-v4-flash",
			"name":            "DeepSeek V4 Flash",
			"maxInputTokens":  1_000_000,
			"maxOutputTokens": 50_000,
			"maxAllowedSize":  1_000_000,
		},
		{
			"id":              "glm-5.3",
			"name":            "GLM",
			"maxAllowedSize":  float64(1_000_000),
			"maxOutputTokens": json.Number("48000"),
		},
	}, "v3_config")
	if len(out) != 2 {
		t.Fatalf("len=%d", len(out))
	}
	if out[0].MaxInputTokens != 1_000_000 || out[0].MaxOutputTokens != 50_000 || out[0].MaxAllowedSize != 1_000_000 {
		t.Fatalf("deepseek=%+v", out[0])
	}
	if out[0].ContextLength() != 1_000_000 {
		t.Fatalf("deepseek context=%d", out[0].ContextLength())
	}
	if out[1].MaxInputTokens != 0 || out[1].MaxAllowedSize != 1_000_000 || out[1].MaxOutputTokens != 48_000 {
		t.Fatalf("glm=%+v", out[1])
	}
	if out[1].ContextLength() != 1_000_000 {
		t.Fatalf("glm context=%d want maxAllowedSize fallback", out[1].ContextLength())
	}
}

func TestPublicModelIDStripsCodeBuddyPrefix(t *testing.T) {
	cases := map[string]string{
		"":                          "auto",
		"default":                   "auto",
		"auto":                      "auto",
		"codebuddy/auto":            "auto",
		"codebuddy:auto":            "auto",
		"codebuddy/deepseek-v4-pro": "deepseek-v4-pro",
		"CODEBUDDY/glm-5.3":         "glm-5.3",
		"minimax-m2.5":              "minimax-m2.5",
	}
	for in, want := range cases {
		if got := PublicModelID(in); got != want {
			t.Fatalf("PublicModelID(%q)=%q want %q", in, got, want)
		}
	}
}
