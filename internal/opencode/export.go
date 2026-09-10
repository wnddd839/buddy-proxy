package opencode

import (
	"strings"

	"github.com/wnddd839/codebuddy-proxy/internal/models"
)

// ReasoningOption mirrors models.dev reasoning_options entries used by OpenCode.
type ReasoningOption struct {
	Type   string   `json:"type"`
	Values []string `json:"values,omitempty"`
}

// ModelExtensions are optional /v1/models fields aligned with models.dev / OpenCode.
type ModelExtensions struct {
	ReasoningOptions []ReasoningOption `json:"reasoning_options,omitempty"`
	Interleaved      map[string]any    `json:"interleaved,omitempty"`
}

// ExtensionsForModel maps upstream /v3/config reasoning metadata to models.dev shape.
func ExtensionsForModel(model models.Model) ModelExtensions {
	if !model.SupportsReasoning {
		return ModelExtensions{}
	}
	ext := ModelExtensions{
		Interleaved: map[string]any{"field": "reasoning_content"},
	}
	if opts := reasoningOptionsFromModel(model); len(opts) > 0 {
		ext.ReasoningOptions = opts
	}
	return ext
}

// ModelListFields returns OpenCode / models.dev compatible metadata for GET /v1/models.
// reasoning must be a boolean (not upstream object) so clients can enable thinking UI.
func ModelListFields(model models.Model) map[string]any {
	fields := map[string]any{}
	if model.SupportsReasoning {
		fields["reasoning"] = true
	}
	if len(model.Reasoning) > 0 {
		fields["reasoning_config"] = model.Reasoning
	}
	ext := ExtensionsForModel(model)
	if len(ext.ReasoningOptions) > 0 {
		fields["reasoning_options"] = ext.ReasoningOptions
	}
	if ext.Interleaved != nil {
		fields["interleaved"] = ext.Interleaved
	}
	if variants := VariantsForModel(model); len(variants) > 0 {
		fields["variants"] = variants
	}
	if n := model.ContextLength(); n > 0 {
		fields["context_length"] = n
		fields["max_input_tokens"] = n
	}
	if model.MaxOutputTokens > 0 {
		fields["max_output_tokens"] = model.MaxOutputTokens
	}
	if limit := tokenLimitObject(model); len(limit) > 0 {
		fields["limit"] = limit
	}
	return fields
}

func tokenLimitObject(model models.Model) map[string]any {
	limit := map[string]any{}
	if n := model.ContextLength(); n > 0 {
		limit["context"] = n
	}
	if model.MaxOutputTokens > 0 {
		limit["output"] = model.MaxOutputTokens
	}
	return limit
}

func reasoningOptionsFromModel(model models.Model) []ReasoningOption {
	if !model.SupportsReasoning {
		return nil
	}
	efforts := supportedEfforts(model.Reasoning)
	if len(efforts) == 0 {
		return nil
	}
	opts := make([]ReasoningOption, 0, 2)
	if canDisableThinking(model.Reasoning) {
		opts = append(opts, ReasoningOption{Type: "toggle"})
	}
	opts = append(opts, ReasoningOption{Type: "effort", Values: efforts})
	return opts
}

func supportedEfforts(reasoning map[string]any) []string {
	if reasoning == nil {
		return nil
	}
	raw, ok := reasoning["supportedEfforts"].([]any)
	if !ok || len(raw) == 0 {
		if effort, ok := reasoning["effort"].(string); ok && strings.TrimSpace(effort) != "" {
			return []string{effort}
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func canDisableThinking(reasoning map[string]any) bool {
	if reasoning == nil {
		return false
	}
	v, ok := reasoning["canDisableThinking"].(bool)
	return ok && v
}

func effortSet(efforts []string) map[string]bool {
	set := make(map[string]bool, len(efforts))
	for _, effort := range efforts {
		set[strings.ToLower(strings.TrimSpace(effort))] = true
	}
	return set
}

// VariantsForModel builds OpenCode variant overlays for @ai-sdk/openai-compatible.
func VariantsForModel(model models.Model) map[string]map[string]any {
	if !model.SupportsReasoning {
		return nil
	}
	efforts := supportedEfforts(model.Reasoning)
	if len(efforts) == 0 {
		efforts = []string{"low", "medium", "high"}
	}
	variants := make(map[string]map[string]any, len(efforts)+1)
	if canDisableThinking(model.Reasoning) {
		variants["none"] = map[string]any{"thinking": map[string]any{"type": "disabled"}}
	}
	for _, effort := range efforts {
		variants[effort] = map[string]any{"reasoningEffort": effort}
	}
	if len(variants) == 0 {
		return nil
	}
	return variants
}

// LiteLLMModelInfoEntry is one row for GET /v1/model/info (opencode-models-discovery litellm enricher).
func LiteLLMModelInfoEntry(model models.Model) map[string]any {
	info := map[string]any{
		"key":  model.ID,
		"mode": "chat",
	}
	if n := model.ContextLength(); n > 0 {
		info["max_input_tokens"] = n
	}
	if model.MaxOutputTokens > 0 {
		info["max_output_tokens"] = model.MaxOutputTokens
		info["max_tokens"] = model.MaxOutputTokens
	}
	if model.SupportsReasoning {
		info["supports_reasoning"] = true
		info["supported_openai_params"] = []string{"reasoning_effort"}
		efforts := supportedEfforts(model.Reasoning)
		set := effortSet(efforts)
		if canDisableThinking(model.Reasoning) {
			info["supports_none_reasoning_effort"] = true
		}
		if len(set) == 0 || set["low"] || set["minimal"] {
			info["supports_low_reasoning_effort"] = true
		}
		if set["xhigh"] {
			info["supports_xhigh_reasoning_effort"] = true
		}
		if set["max"] {
			info["supports_max_reasoning_effort"] = true
		}
	}
	return map[string]any{
		"model_name": model.ID,
		"litellm_params": map[string]any{
			"model": model.ID,
		},
		"model_info": info,
	}
}
