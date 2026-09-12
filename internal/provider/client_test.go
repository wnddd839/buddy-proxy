package provider_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

func TestResolveProtocolDirectBillingEndpoint(t *testing.T) {
	opts := provider.ChatOptions{
		Site:        "domestic",
		APIEndpoint: "https://copilot.tencent.com/v2/chat/completions",
	}
	got := provider.ResolveProtocolDirectBillingEndpoint(opts, "/v2/billing/meter/daily-checkin")
	want := "https://copilot.tencent.com/v2/billing/meter/daily-checkin"
	if got != want {
		t.Fatalf("billing endpoint=%s want=%s", got, want)
	}

	opts.APIEndpoint = "https://copilot.tencent.com/v2"
	got = provider.ResolveProtocolDirectBillingEndpoint(opts, "/v2/billing/meter/daily-checkin")
	want = "https://copilot.tencent.com/v2/billing/meter/daily-checkin"
	if got != want {
		t.Fatalf("dedup billing endpoint=%s want=%s", got, want)
	}
}

func TestResolveProtocolDirectDomestic(t *testing.T) {
	opts := provider.ChatOptions{
		Site:    "domestic",
		BaseURL: "https://www.codebuddy.cn",
		Domain:  "www.codebuddy.cn", // portal host must not win over chat endpoint
	}
	endpoint := provider.ResolveProtocolDirectEndpoint(opts)
	if endpoint != "https://copilot.tencent.com/v2/chat/completions" {
		t.Fatalf("unexpected endpoint: %s", endpoint)
	}
	domain := provider.ResolveProtocolDirectDomain(opts)
	if domain != "copilot.tencent.com" {
		t.Fatalf("unexpected domain: %s", domain)
	}
}

func TestAccountSiteBeatsProxyBaseURL(t *testing.T) {
	// Domestic account must stay on CN chat host even if process BaseURL is global
	// (overseas VPS / VPN / mis-set CODEBUDDY_BASE_URL).
	domestic := provider.ChatOptions{
		Site:        "domestic",
		BaseURL:     "https://www.codebuddy.ai",
		APIEndpoint: "https://www.codebuddy.ai/v2/chat/completions",
	}
	if provider.RegionOf(domestic) != "domestic" {
		t.Fatalf("region=%s", provider.RegionOf(domestic))
	}
	if got := provider.ResolveProtocolDirectEndpoint(domestic); got != "https://copilot.tencent.com/v2/chat/completions" {
		t.Fatalf("domestic endpoint=%s", got)
	}
	if got := provider.ResolveProtocolDirectDomain(domestic); got != "copilot.tencent.com" {
		t.Fatalf("domestic domain=%s", got)
	}

	global := provider.ChatOptions{
		Site:    "global",
		BaseURL: "https://www.codebuddy.cn",
	}
	// Explicit global site wins even if BaseURL looks domestic.
	if provider.RegionOf(global) != "global" {
		t.Fatalf("region=%s", provider.RegionOf(global))
	}
	if got := provider.ResolveProtocolDirectEndpoint(global); got != "https://www.codebuddy.ai/v2/chat/completions" {
		t.Fatalf("global endpoint=%s", got)
	}
}

func TestResolveProtocolDirectWorkBuddy(t *testing.T) {
	domestic := provider.ChatOptions{Site: "domestic", Product: "workbuddy"}
	if got := provider.ResolveProtocolDirectEndpoint(domestic); got != "https://www.workbuddy.cn/v2/chat/completions" {
		t.Fatalf("domestic workbuddy endpoint=%s", got)
	}
	if got := provider.ResolveProtocolDirectDomain(domestic); got != "www.workbuddy.cn" {
		t.Fatalf("domestic workbuddy domain=%s", got)
	}

	global := provider.ChatOptions{Site: "global", Product: "workbuddy", BaseURL: "https://www.codebuddy.ai"}
	if got := provider.ResolveProtocolDirectEndpoint(global); got != "https://www.workbuddy.ai/v2/chat/completions" {
		t.Fatalf("global workbuddy endpoint=%s", got)
	}
	if got := provider.ResolveProtocolDirectDomain(global); got != "www.workbuddy.ai" {
		t.Fatalf("global workbuddy domain=%s", got)
	}

	if got := provider.AlignAPIEndpoint("workbuddy", "https://www.codebuddy.ai/v2/chat/completions"); got != "" {
		t.Fatalf("codebuddy endpoint must not follow workbuddy product: %s", got)
	}
	if got := provider.AlignAPIEndpoint("workbuddy", "https://www.workbuddy.ai/v2/chat/completions"); got != "https://www.workbuddy.ai/v2/chat/completions" {
		t.Fatalf("workbuddy endpoint kept=%s", got)
	}
}

func TestBuildProtocolDirectHeadersProduct(t *testing.T) {
	client := provider.NewClient(config.Config{})
	cli, _ := client.BuildProtocolDirectHeaders(provider.ChatOptions{
		Site: "global", Product: "codebuddy", BearerToken: "tok", UserID: "u1",
	})
	if cli.Get("X-IDE-Type") != "CLI" {
		t.Fatalf("codebuddy ide type=%s", cli.Get("X-IDE-Type"))
	}
	if !strings.Contains(cli.Get("User-Agent"), "CLI/") || !strings.Contains(cli.Get("User-Agent"), "CodeBuddy/") {
		t.Fatalf("codebuddy ua=%s", cli.Get("User-Agent"))
	}

	wb, wbTrace := client.BuildProtocolDirectHeaders(provider.ChatOptions{
		Site: "global", Product: "workbuddy", BearerToken: "tok", UserID: "u1",
	})
	if wbTrace.ConversationRequestID == "" || wbTrace.ConversationID == "" {
		t.Fatalf("trace=%+v", wbTrace)
	}
	if wb.Get("X-IDE-Type") != "VSCode" || wb.Get("X-IDE-Name") != "VSCode" {
		t.Fatalf("workbuddy ide=%s/%s", wb.Get("X-IDE-Type"), wb.Get("X-IDE-Name"))
	}
	if wb.Get("User-Agent") != "VSCode/1.119.0 WorkBuddy/4.9.29177644" {
		t.Fatalf("workbuddy ua=%s", wb.Get("User-Agent"))
	}
	if wb.Get("X-Product-Version") != "4.9.29177644" || wb.Get("X-Env-ID") != "production" {
		t.Fatalf("workbuddy product headers version=%s env=%s", wb.Get("X-Product-Version"), wb.Get("X-Env-ID"))
	}
	if wb.Get("X-Domain") != "www.workbuddy.ai" {
		t.Fatalf("workbuddy domain=%s", wb.Get("X-Domain"))
	}
}

func TestNormalizeToolChoice(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want any
	}{
		{"string auto", "auto", "auto"},
		{"object auto", map[string]any{"type": "auto"}, "auto"},
		{"object none", map[string]any{"type": "none"}, "none"},
		{"object function", map[string]any{"type": "function", "function": map[string]any{"name": "bash"}}, "auto"},
		{"nil", nil, nil},
		{"empty string", "", nil},
	}
	for _, tc := range cases {
		got := provider.NormalizeToolChoice(tc.in)
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Fatalf("%s: got %#v want %#v", tc.name, got, tc.want)
		}
	}
}

func TestApplyReasoningFieldsMapsEffort(t *testing.T) {
	body := map[string]any{"model": "auto"}
	provider.ApplyReasoningFields(body, provider.ChatOptions{ReasoningEffort: "high"})
	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "high" {
		t.Fatalf("reasoning=%v", body["reasoning"])
	}
	if body["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort=%v", body["reasoning_effort"])
	}
}

func TestApplyReasoningFieldsPrefersReasoningObject(t *testing.T) {
	body := map[string]any{"model": "auto"}
	explicit := map[string]any{"effort": "low", "summary": "auto"}
	provider.ApplyReasoningFields(body, provider.ChatOptions{
		ReasoningEffort: "high",
		Reasoning:       explicit,
	})
	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "low" || reasoning["summary"] != "auto" {
		t.Fatalf("reasoning=%v", body["reasoning"])
	}
}

func TestNormalizeModelsPreservesReasoning(t *testing.T) {
	rows := provider.NormalizeModels(map[string]any{
		"data": map[string]any{
			"models": []any{
				map[string]any{
					"id": "glm-5.3-flash", "name": "GLM", "supportsToolCall": true,
					"supportsReasoning": true, "onlyReasoning": true,
					"reasoning": map[string]any{
						"defaultEffort": "high", "supportedEfforts": []any{"low", "high", "max"},
					},
				},
			},
		},
	})
	if len(rows) != 1 {
		t.Fatalf("expected 1 model, got %d", len(rows))
	}
	if rows[0]["supportsReasoning"] != true {
		t.Fatalf("supportsReasoning=%v", rows[0]["supportsReasoning"])
	}
	reasoning, ok := rows[0]["reasoning"].(map[string]any)
	if !ok || reasoning["defaultEffort"] != "high" {
		t.Fatalf("reasoning=%v", rows[0]["reasoning"])
	}
}

func TestEventsFromOpenAIChunkReasoningContent(t *testing.T) {
	chunk := provider.MapSSEEvent(map[string]any{
		"choices": []any{
			map[string]any{
				"delta": map[string]any{"reasoning_content": "think"},
			},
		},
	})
	if len(chunk) != 1 || chunk[0].Type != "thinking_delta" || chunk[0].Text != "think" {
		t.Fatalf("unexpected events: %+v", chunk)
	}
}

func TestNormalizeModelsFromV3Shape(t *testing.T) {
	rows := provider.NormalizeModels(map[string]any{
		"data": map[string]any{
			"models": map[string]any{
				"availableModels": []any{"auto", "glm-5.2"},
				"models": []any{
					map[string]any{"id": "auto", "name": "Auto", "supportsToolCall": true},
					map[string]any{"id": "glm-5.2", "name": "GLM", "supportsToolCall": true},
					map[string]any{"id": "hidden", "name": "Hidden"},
				},
			},
		},
	})
	if len(rows) != 2 {
		t.Fatalf("expected 2 models, got %d (%v)", len(rows), rows)
	}
}

func TestMapSSEOpenAIDelta(t *testing.T) {
	events := provider.MapSSEEvent(map[string]any{
		"choices": []any{
			map[string]any{
				"delta": map[string]any{"content": "hello"},
			},
		},
	})
	if len(events) != 1 || events[0].Type != "text_delta" || events[0].Text != "hello" {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestDescribeUpstreamBodyMasksPrompt(t *testing.T) {
	body := map[string]any{
		"model": "hy4-preview",
		"messages": []map[string]any{
			{"role": "system", "content": "You are a large language model trained by Microsoft."},
			{"role": "user", "content": "hi"},
		},
		"temperature": 1.0,
		"thinking":    map[string]any{"type": "enabled", "budget_tokens": 10000},
		"tools": []any{
			map[string]any{"type": "function", "function": map[string]any{"name": "bash", "arguments": "{}"}},
		},
	}
	fp := provider.DescribeUpstreamBody(body)
	if fp["model"] != "hy4-preview" {
		t.Fatalf("model=%v", fp["model"])
	}
	roles, _ := fp["roles"].([]string)
	if len(roles) != 2 || roles[0] != "system" || roles[1] != "user" {
		t.Fatalf("roles=%v", fp["roles"])
	}
	names, _ := fp["toolNames"].([]string)
	if len(names) != 1 || names[0] != "bash" {
		t.Fatalf("toolNames=%v", fp["toolNames"])
	}
	msgs, _ := fp["messages"].([]map[string]any)
	if len(msgs) != 2 {
		t.Fatalf("messages=%v", fp["messages"])
	}
	preview, _ := msgs[0]["preview"].(string)
	if len(preview) > 200 || preview == "" {
		t.Fatalf("preview len=%d", len(preview))
	}
	if _, ok := fp["temperature"]; !ok {
		t.Fatalf("temperature missing: %v", fp)
	}
}

func TestEnsureUpstreamMessagesStripsPRsNote(t *testing.T) {
	const trigger = "Main branch (you will usually use this for PRs): main"
	out := provider.EnsureUpstreamMessages([]map[string]any{
		{"role": "system", "content": trigger},
		{"role": "assistant", "content": trigger},
		{"role": "user", "content": trigger},
	})
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
	if out[0]["content"] != "Main branch: main" {
		t.Fatalf("system not sanitized: %q", out[0]["content"])
	}
	if out[1]["content"] != trigger {
		t.Fatalf("assistant must pass through: %q", out[1]["content"])
	}
	if out[2]["content"] != trigger {
		t.Fatalf("user must pass through: %q", out[2]["content"])
	}
}

func TestDescribeUpstreamBodyStructuredContent(t *testing.T) {
	body := map[string]any{
		"model": "hy4-preview",
		"messages": []map[string]any{
			{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "hi"},
				map[string]any{"type": "input_audio", "data": "xxx"},
				"raw-string-part",
			}},
		},
	}
	fp := provider.DescribeUpstreamBody(body)
	msgs, _ := fp["messages"].([]map[string]any)
	if len(msgs) != 1 {
		t.Fatalf("messages=%v", fp["messages"])
	}
	if msgs[0]["contentKind"] != "structured_array" {
		t.Fatalf("contentKind=%v", msgs[0]["contentKind"])
	}
	parts, _ := msgs[0]["partTypes"].([]string)
	if len(parts) != 3 || parts[0] != "text" || parts[1] != "input_audio" || parts[2] != "string" {
		t.Fatalf("partTypes=%v", msgs[0]["partTypes"])
	}
}

func TestEnsureUpstreamMessagesDropsEmpty(t *testing.T) {
	out := provider.EnsureUpstreamMessages([]map[string]any{
		{"role": "user", "content": ""},
		{"role": "user", "content": "hi"},
	})
	if len(out) != 1 {
		t.Fatalf("expected 1 message, got %d", len(out))
	}
}

func TestEnsureUpstreamMessagesMapsDeveloper(t *testing.T) {
	out := provider.EnsureUpstreamMessages([]map[string]any{
		{"role": "developer", "content": "You are a coding agent."},
		{"role": "user", "content": "hi"},
	})
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d (%v)", len(out), out)
	}
	if out[0]["role"] != "system" {
		t.Fatalf("developer role=%v want system", out[0]["role"])
	}
}

func TestNormalizeModelsPreservesCredits(t *testing.T) {
	rows := provider.NormalizeModels(map[string]any{
		"data": map[string]any{
			"models": []any{
				map[string]any{"id": "hy4-preview", "name": "Hy4 preview", "credits": "x0.00 credits", "supportsToolCall": true},
				map[string]any{"id": "hy4-preview-x", "name": "Hy4 preview", "credits": "x0.29 credits", "supportsToolCall": true},
			},
		},
	})
	if len(rows) != 2 {
		t.Fatalf("expected 2 models, got %d (%v)", len(rows), rows)
	}
	if rows[0]["credits"] != "x0.00 credits" || rows[0]["free"] != true {
		t.Fatalf("free model extras: %+v", rows[0])
	}
	if rows[1]["credits"] != "x0.29 credits" || rows[1]["creditMultiplier"] != 0.29 {
		t.Fatalf("paid model extras: %+v", rows[1])
	}
}

func TestNormalizeModelsPreservesContextLimits(t *testing.T) {
	rows := provider.NormalizeModels(map[string]any{
		"data": map[string]any{
			"models": []any{
				map[string]any{
					"id":               "deepseek-v4-flash",
					"name":             "DeepSeek V4 Flash",
					"maxInputTokens":   float64(1_000_000),
					"maxOutputTokens":  float64(50_000),
					"maxAllowedSize":   float64(1_000_000),
					"supportsToolCall": true,
				},
				map[string]any{
					"id":              "hy4-preview",
					"name":            "HY4",
					"maxInputTokens":  1_000_000,
					"maxOutputTokens": 64_000,
				},
			},
		},
	})
	if len(rows) != 2 {
		t.Fatalf("expected 2 models, got %d", len(rows))
	}
	if rows[0]["maxInputTokens"] != 1_000_000 || rows[0]["maxOutputTokens"] != 50_000 || rows[0]["maxAllowedSize"] != 1_000_000 {
		t.Fatalf("deepseek limits: %+v", rows[0])
	}
	if rows[1]["maxInputTokens"] != 1_000_000 || rows[1]["maxOutputTokens"] != 64_000 {
		t.Fatalf("hy4 limits: %+v", rows[1])
	}
	if _, ok := rows[1]["maxAllowedSize"]; ok {
		t.Fatalf("unexpected maxAllowedSize on hy4: %+v", rows[1])
	}
}

func TestParseCreditMultiplier(t *testing.T) {
	n, ok := provider.ParseCreditMultiplier("x0.29 credits")
	if !ok || n != 0.29 {
		t.Fatalf("got %v %v", n, ok)
	}
	n, ok = provider.ParseCreditMultiplier("x0.00 credits")
	if !ok || n != 0 {
		t.Fatalf("got %v %v", n, ok)
	}
}
