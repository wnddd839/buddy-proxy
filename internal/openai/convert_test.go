package openai

import (
	"encoding/json"
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
