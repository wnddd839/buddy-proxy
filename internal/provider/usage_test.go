package provider

import "testing"

func TestEstimateUsageUsesPromptLength(t *testing.T) {
	prompt := string(make([]byte, 40)) // ASCII
	usage := estimateUsage(prompt, "xxxx")
	if usage.PromptTokens != 10 { // (40+3)/4 = 10
		t.Fatalf("prompt tokens=%d want 10", usage.PromptTokens)
	}
	if usage.Source != "estimated" {
		t.Fatalf("source=%q want estimated", usage.Source)
	}
	if usage.CompletionTokens < 1 {
		t.Fatal("completion tokens should be >= 1")
	}
}

func TestEstimateTokenCountCJK(t *testing.T) {
	// 4 个汉字 = 12 字节；旧公式 (12+3)/4=3，应按字计为 4。
	zh := "你好世界"
	got := estimateTokenCount(zh)
	if got != 4 {
		t.Fatalf("cjk tokens=%d want 4 (bytes=%d)", got, len(zh))
	}
	en := "hello world" // 11 ascii → (11+3)/4 = 3
	if estimateTokenCount(en) != 3 {
		t.Fatalf("ascii tokens=%d want 3", estimateTokenCount(en))
	}
}

func TestSnapshotUsesPromptInsteadOfEmpty(t *testing.T) {
	acc := newAccumulator()
	acc.push(Event{Type: "text_delta", Text: "hi"})
	turn := acc.snapshot(func() string { return string(make([]byte, 40)) })
	if turn.Usage.PromptTokens != 10 {
		t.Fatalf("prompt_tokens=%d want 10 (not empty-prompt floor)", turn.Usage.PromptTokens)
	}
	if turn.Usage.Source != "estimated" {
		t.Fatalf("source=%q", turn.Usage.Source)
	}
}

func TestSnapshotPrefersUpstreamUsage(t *testing.T) {
	acc := newAccumulator()
	acc.push(Event{Type: "text_delta", Text: "hi"})
	acc.push(Event{Type: "usage", Usage: &Usage{PromptTokens: 123, CompletionTokens: 4, TotalTokens: 127}})
	called := false
	turn := acc.snapshot(func() string {
		called = true
		return "should-not-run"
	})
	if called {
		t.Fatal("promptFn must not run when upstream usage present")
	}
	if turn.Usage.PromptTokens != 123 || turn.Usage.CompletionTokens != 4 {
		t.Fatalf("unexpected usage %+v", turn.Usage)
	}
	if turn.Usage.Source != "upstream" {
		t.Fatalf("source=%q want upstream", turn.Usage.Source)
	}
}

func TestParseUsagePreservesCacheFields(t *testing.T) {
	usage := ParseUsage(map[string]any{
		"prompt_tokens":     float64(1000),
		"completion_tokens": float64(50),
		"total_tokens":      float64(1050),
		"prompt_tokens_details": map[string]any{
			"cached_tokens": float64(800),
		},
		"cache_read_input_tokens":     float64(800),
		"cache_creation_input_tokens": float64(20),
		"prompt_cache_hit_tokens":     float64(800),
	})
	if usage.PromptTokens != 1000 || usage.CompletionTokens != 50 || usage.TotalTokens != 1050 {
		t.Fatalf("base fields %+v", usage)
	}
	if usage.CachedTokens() != 800 {
		t.Fatalf("cached=%d", usage.CachedTokens())
	}
	if usage.PromptTokensDetails == nil || usage.PromptTokensDetails.CachedTokens != 800 {
		t.Fatalf("details=%+v", usage.PromptTokensDetails)
	}
	if usage.CacheCreationInputTokens != 20 {
		t.Fatalf("cache creation=%d", usage.CacheCreationInputTokens)
	}
}

func TestParseUsageDeepSeekAliases(t *testing.T) {
	usage := ParseUsage(map[string]any{
		"prompt_tokens":            float64(200),
		"completion_tokens":        float64(10),
		"prompt_cache_hit_tokens":  float64(150),
		"prompt_cache_miss_tokens": float64(50),
	})
	if usage.CachedTokens() != 150 {
		t.Fatalf("cached=%d", usage.CachedTokens())
	}
	if usage.PromptTokensDetails == nil || usage.PromptTokensDetails.CachedTokens != 150 {
		t.Fatalf("normalized details=%+v", usage.PromptTokensDetails)
	}
}

func TestParseUsageWorkBuddyCamelCase(t *testing.T) {
	usage := ParseUsage(map[string]any{
		"prompt_tokens":          float64(1200),
		"completion_tokens":      float64(40),
		"cacheReadInputTokens":   float64(900),
		"promptCacheHitTokens":   float64(900),
		"promptTokensDetails": map[string]any{
			"cachedTokens": float64(900),
		},
	})
	if usage.CachedTokens() != 900 {
		t.Fatalf("workbuddy camelCase cached=%d want 900", usage.CachedTokens())
	}
}

func TestParseUsageNestedCacheTokensAlias(t *testing.T) {
	usage := ParseUsage(map[string]any{
		"promptTokens": float64(500),
		"inputTokensDetails": map[string]any{
			"cache_tokens": float64(320),
		},
	})
	if usage.PromptTokens != 500 {
		t.Fatalf("prompt=%d", usage.PromptTokens)
	}
	if usage.CachedTokens() != 320 {
		t.Fatalf("cached=%d want 320", usage.CachedTokens())
	}
}

func TestParseUsageTopLevelAndInputDetails(t *testing.T) {
	u1 := ParseUsage(map[string]any{
		"prompt_tokens": float64(100),
		"cached_tokens": float64(40),
	})
	if u1.CachedTokens() != 40 {
		t.Fatalf("top-level cached=%d", u1.CachedTokens())
	}
	u2 := ParseUsage(map[string]any{
		"prompt_tokens": float64(100),
		"input_tokens_details": map[string]any{
			"cache_hit_tokens": float64(55),
		},
	})
	if u2.CachedTokens() != 55 {
		t.Fatalf("input_tokens_details cached=%d", u2.CachedTokens())
	}
}

func TestUsageEventFromPayload(t *testing.T) {
	ev := usageEventFromPayload(map[string]any{
		"usage": map[string]any{
			"prompt_tokens":         float64(10),
			"completion_tokens":     float64(2),
			"prompt_tokens_details": map[string]any{"cached_tokens": float64(7)},
		},
	})
	if ev == nil || ev.Usage == nil || ev.Usage.CachedTokens() != 7 {
		t.Fatalf("event=%+v", ev)
	}
}

func TestEventsFromOpenAIChunk(t *testing.T) {
	chunk := openAISSEChunk{
		Choices: []openAISSEChoice{{
			Delta:        &openAISSEDelta{Content: "你好"},
			FinishReason: "stop",
		}},
		Usage: map[string]any{
			"prompt_tokens":     float64(3),
			"completion_tokens": float64(1),
			"total_tokens":      float64(4),
		},
	}
	events := eventsFromOpenAIChunk(chunk)
	if len(events) < 3 {
		t.Fatalf("events=%d want >=3 %+v", len(events), events)
	}
	foundUsage, foundText, foundEnd := false, false, false
	for _, ev := range events {
		switch ev.Type {
		case "usage":
			foundUsage = ev.Usage != nil && ev.Usage.PromptTokens == 3
		case "text_delta":
			foundText = ev.Text == "你好"
		case "turn_ended":
			foundEnd = ev.StopReason == "stop"
		}
	}
	if !foundUsage || !foundText || !foundEnd {
		t.Fatalf("missing events: usage=%v text=%v end=%v %+v", foundUsage, foundText, foundEnd, events)
	}
}
