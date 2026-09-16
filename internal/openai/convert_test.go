package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

func TestUsageFromProviderPreservesCache(t *testing.T) {
	u := UsageFromProvider(provider.Usage{
		PromptTokens:         100,
		CompletionTokens:     20,
		TotalTokens:          120,
		PromptTokensDetails:  &provider.PromptTokensDetails{CachedTokens: 70},
		CacheReadInputTokens: 70,
	})
	if u.PromptTokensDetails == nil || u.PromptTokensDetails.CachedTokens != 70 {
		t.Fatalf("%+v", u)
	}
	if u.PromptCacheHitTokens != 70 || u.CacheReadInputTokens != 70 {
		t.Fatalf("aliases not filled: hit=%d read=%d", u.PromptCacheHitTokens, u.CacheReadInputTokens)
	}
	if u.PromptCacheMissTokens != 30 {
		t.Fatalf("miss=%d want 30", u.PromptCacheMissTokens)
	}
	raw, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !contains(s, `"cached_tokens":70`) || !contains(s, `"prompt_tokens":100`) {
		t.Fatalf("json=%s", s)
	}
	if !contains(s, `"prompt_cache_hit_tokens":70`) || !contains(s, `"cache_read_input_tokens":70`) {
		t.Fatalf("missing aliases json=%s", s)
	}
}

func TestUsageFromProviderFillsAliasesFromHitOnly(t *testing.T) {
	u := UsageFromProvider(provider.Usage{
		PromptTokens:         200,
		CompletionTokens:     10,
		TotalTokens:          210,
		PromptCacheHitTokens: 150,
	})
	raw, _ := json.Marshal(u)
	s := string(raw)
	if !contains(s, `"cached_tokens":150`) || !contains(s, `"prompt_cache_hit_tokens":150`) || !contains(s, `"cache_read_input_tokens":150`) {
		t.Fatalf("json=%s", s)
	}
	if !contains(s, `"prompt_cache_miss_tokens":50`) {
		t.Fatalf("miss not derived json=%s", s)
	}
}

func TestUsageFromProviderWorkBuddyLiveBackfill(t *testing.T) {
	parsed := provider.ParseUsage(map[string]any{
		"prompt_tokens":            float64(1576),
		"completion_tokens":        float64(1),
		"total_tokens":             float64(1577),
		"prompt_tokens_details":    map[string]any{"cached_tokens": float64(1536)},
		"prompt_cache_hit_tokens":  float64(1536),
		"prompt_cache_miss_tokens": float64(40),
		"cache_read_input_tokens":  float64(0),
		"cached_tokens":            float64(0),
	})
	u := UsageFromProvider(parsed)
	if u.PromptTokensDetails == nil || u.PromptTokensDetails.CachedTokens != 1536 {
		t.Fatalf("details %+v", u.PromptTokensDetails)
	}
	if u.PromptCacheHitTokens != 1536 || u.CacheReadInputTokens != 1536 {
		t.Fatalf("hit=%d read=%d want 1536 (downstream often only looks at cache_read)", u.PromptCacheHitTokens, u.CacheReadInputTokens)
	}
	if u.PromptCacheMissTokens != 40 {
		t.Fatalf("miss=%d", u.PromptCacheMissTokens)
	}
}

func TestStreamUsageChunk(t *testing.T) {
	chunk := StreamUsageChunk("id1", "auto", Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3})
	if chunk.Usage == nil || chunk.Usage.TotalTokens != 3 {
		t.Fatalf("%+v", chunk)
	}
	if len(chunk.Choices) != 0 {
		t.Fatalf("choices should be empty for include_usage chunk")
	}
	raw, _ := json.Marshal(chunk)
	if !contains(string(raw), `"usage"`) {
		t.Fatalf("%s", raw)
	}
}

func TestClassifyCause(t *testing.T) {
	tests := []struct {
		err  string
		want string
	}{
		{"CodeBuddy chat completion failed with 502: <html>openresty", "upstream_infra"},
		{"failed with 429: too many requests", "rate_limited"},
		{"unapproved channel 11128", "account_blocked"},
		{"request illegal 11140", "account_blocked"},
		{"failed with 401: unauthorized", "account_blocked"},
		{"failed with 400: bad request", ""},
	}
	for _, tc := range tests {
		if got := ClassifyCause(errors.New(tc.err)); got != tc.want {
			t.Fatalf("ClassifyCause(%q)=%q want %q", tc.err, got, tc.want)
		}
	}
}

func TestRetryAfterUnwrapsWrappedChatError(t *testing.T) {
	inner := &provider.ChatError{Status: 429, RetryAfter: "8", Msg: "failed with 429"}
	wrapped := fmt.Errorf("retry: %w", inner)
	if got := RetryAfter(wrapped); got != "8" {
		t.Fatalf("RetryAfter wrapped=%q", got)
	}
}

func TestToolCallDeltaIndexZeroMustBePresent(t *testing.T) {
	chunk := StreamChunkOf("id1", "auto", Delta{
		ToolCalls: []ToolCallDelta{{
			Index: 0,
			ID:    "call_1",
			Type:  "function",
			Function: ToolFunction{
				Name:      "read_file",
				Arguments: `{"path":"a.go"}`,
			},
		}},
	}, nil)
	raw, err := json.Marshal(chunk)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !contains(s, `"index":0`) {
		t.Fatalf("streaming tool_calls must emit index even when 0; json=%s", s)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	choices, _ := parsed["choices"].([]any)
	if len(choices) == 0 {
		t.Fatal("missing choices")
	}
	choice, _ := choices[0].(map[string]any)
	delta, _ := choice["delta"].(map[string]any)
	toolCalls, _ := delta["tool_calls"].([]any)
	if len(toolCalls) == 0 {
		t.Fatal("missing tool_calls")
	}
	tc, _ := toolCalls[0].(map[string]any)
	if _, ok := tc["index"]; !ok {
		t.Fatalf("tool_calls[0].index missing; keys=%v json=%s", keysOf(tc), s)
	}
}

func TestFromTurnToolCallsOmitIndex(t *testing.T) {
	out := FromTurn(provider.Turn{
		Text: "",
		ToolUses: []provider.ToolUse{{
			ID:    "call_1",
			Name:  "read_file",
			Input: map[string]any{"path": "a.go"},
		}},
	}, "id1", "auto")
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	choices, _ := parsed["choices"].([]any)
	if len(choices) == 0 {
		t.Fatal("missing choices")
	}
	choice, _ := choices[0].(map[string]any)
	msg, _ := choice["message"].(map[string]any)
	toolCalls, _ := msg["tool_calls"].([]any)
	if len(toolCalls) == 0 {
		t.Fatal("missing tool_calls")
	}
	tc, _ := toolCalls[0].(map[string]any)
	if _, ok := tc["index"]; ok {
		t.Fatalf("non-stream message.tool_calls must not emit index; keys=%v json=%s", keysOf(tc), raw)
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && (indexOf(s, sub) >= 0)))
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
