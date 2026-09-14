package models

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func TestV3ConfigCandidateBasesWorkBuddyDoesNotMergeCopilot(t *testing.T) {
	global := v3ConfigCandidateBases(provider.ChatOptions{
		Site:    "global",
		Product: "workbuddy",
		BaseURL: "https://www.workbuddy.ai",
	})
	if len(global) != 1 || global[0] != "https://www.workbuddy.ai" {
		t.Fatalf("workbuddy global bases=%v", global)
	}
	domestic := v3ConfigCandidateBases(provider.ChatOptions{Site: "domestic", Product: "workbuddy"})
	if len(domestic) != 1 || domestic[0] != "https://www.workbuddy.cn" {
		t.Fatalf("workbuddy domestic bases=%v", domestic)
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

func TestEnrichModelsFillsCLIReasoningFromIDE(t *testing.T) {
	cli := []map[string]any{
		{
			"id":            "deepseek-v4.1-flash",
			"onlyReasoning": true,
			"reasoning":     map[string]any{"effort": "high", "summary": "auto"},
		},
		{
			"id":            "deepseek-v4-flash",
			"onlyReasoning": true,
			"reasoning":     map[string]any{"effort": "high"},
		},
	}
	ide := []map[string]any{
		{
			"id": "deepseek-v4.1-flash",
			"reasoning": map[string]any{
				"canDisableThinking": true,
				"defaultEffort":      "high",
				"supportedEfforts":   []any{"low", "high", "xhigh"},
			},
		},
	}
	out := enrichModelsWithIDEReasoning(cli, ide)
	v41 := out[0]["reasoning"].(map[string]any)
	if v41["canDisableThinking"] != true {
		t.Fatalf("v4.1 canDisableThinking=%v", v41["canDisableThinking"])
	}
	if v41["effort"] != "high" {
		t.Fatalf("v4.1 effort should stay CLI value: %v", v41["effort"])
	}
	efforts, _ := v41["supportedEfforts"].([]any)
	if len(efforts) != 3 {
		t.Fatalf("v4.1 supportedEfforts=%v", v41["supportedEfforts"])
	}
	v4 := out[1]["reasoning"].(map[string]any)
	if v4["canDisableThinking"] != true {
		t.Fatalf("CLI-only flash should still expose canDisableThinking: %v", v4)
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

func TestDedupeByPublicIDMergesDefaultIntoAuto(t *testing.T) {
	out := mergeModelsByPublicID([]map[string]any{
		{"id": "auto", "credits": "1"},
		{"id": "default", "credits": "1"},
		{"id": "hy4-preview"},
		{"id": "codebuddy/hy4-preview"},
	})
	if len(out) != 2 {
		t.Fatalf("len=%d want 2: %+v", len(out), out)
	}
	if modelRowID(out[0]) != "auto" || modelRowID(out[1]) != "hy4-preview" {
		t.Fatalf("ids=%s,%s", modelRowID(out[0]), modelRowID(out[1]))
	}
}

func TestMergeModelsByPublicIDPrefersAutoOverDefault(t *testing.T) {
	out := mergeModelsByPublicID(
		[]map[string]any{{"id": "default", "credits": "from-default"}},
		[]map[string]any{{"id": "auto", "credits": "from-auto"}},
	)
	if len(out) != 1 || modelRowID(out[0]) != "auto" {
		t.Fatalf("want auto row, got %+v", out)
	}
	if got, _ := out[0]["credits"].(string); got != "from-auto" {
		t.Fatalf("credits=%v", out[0]["credits"])
	}
}

func TestConsoleCatalogKeepsIDEReasoningEnrich(t *testing.T) {
	console := []map[string]any{
		{
			"id":        "deepseek-v4.1-flash",
			"reasoning": map[string]any{"effort": "high"},
		},
	}
	ide := []map[string]any{
		{
			"id": "deepseek-v4.1-flash",
			"reasoning": map[string]any{
				"canDisableThinking": true,
				"supportedEfforts":   []any{"low", "high"},
			},
		},
	}
	out := enrichModelsWithIDEReasoning(console, ide)
	got := out[0]["reasoning"].(map[string]any)
	if got["canDisableThinking"] != true {
		t.Fatalf("console path must still receive IDE reasoning: %+v", got)
	}
}

func TestToAdminModelsConsoleSourceIsVerified(t *testing.T) {
	out := ToAdminModels([]map[string]any{{"id": "glm-5.3", "name": "GLM"}}, "console")
	if len(out) != 1 || !out[0].Verified {
		t.Fatalf("console models should be verified: %+v", out)
	}
}

func TestWithCLIIdentityDoesNotMutateCallerHeaders(t *testing.T) {
	orig := map[string]string{"X-Product-Version": "keep-me", "X-IDE-Type": "VSCode"}
	in := provider.ChatOptions{ExtraHeaders: orig}
	out := withCLIIdentity(in)
	if orig["X-IDE-Type"] != "VSCode" {
		t.Fatal("caller ExtraHeaders mutated")
	}
	if out.ExtraHeaders["X-IDE-Type"] != "CLI" {
		t.Fatalf("cli type=%q", out.ExtraHeaders["X-IDE-Type"])
	}
	if _, ok := out.ExtraHeaders["X-Product-Version"]; ok {
		t.Fatal("CLI identity should drop WorkBuddy product header")
	}
}

func TestFetchConsoleDualHostMergesIDs(t *testing.T) {
	primary := httptest.NewServer(consoleCatalogHandler(http.StatusOK, "gpt-5.4", "auto"))
	defer primary.Close()
	secondary := httptest.NewServer(consoleCatalogHandler(http.StatusOK, "hy4-preview", "default"))
	defer secondary.Close()

	got := listConsoleViaHosts(t, primary, secondary)
	if got.ModelsSource != "console" {
		t.Fatalf("modelsSource=%q want console; message=%q", got.ModelsSource, got.Message)
	}
	ids := modelIDSet(got)
	if _, ok := ids["gpt-5.4"]; !ok {
		t.Fatalf("missing gpt-5.4: %v", ids)
	}
	if _, ok := ids["hy4-preview"]; !ok {
		t.Fatalf("missing hy4-preview: %v", ids)
	}
	if _, ok := ids["auto"]; !ok {
		t.Fatalf("missing auto (default should merge): %v", ids)
	}
	if _, ok := ids["default"]; ok {
		t.Fatalf("default should not appear as a public id: %v", ids)
	}
	if len(ids) != 3 {
		t.Fatalf("id set=%v want 3", ids)
	}
	if strings.Contains(got.Message, "Partial console host errors") {
		t.Fatalf("both hosts succeeded, unexpected note: %q", got.Message)
	}
}

func TestFetchConsoleDualHostPartialFailureInMessage(t *testing.T) {
	primary := httptest.NewServer(consoleCatalogHandler(http.StatusInternalServerError))
	defer primary.Close()
	secondary := httptest.NewServer(consoleCatalogHandler(http.StatusOK, "hy4-preview", "glm-5.3"))
	defer secondary.Close()

	got := listConsoleViaHosts(t, primary, secondary)
	if got.ModelsSource != "console" {
		t.Fatalf("modelsSource=%q want console; message=%q", got.ModelsSource, got.Message)
	}
	ids := modelIDSet(got)
	if _, ok := ids["hy4-preview"]; !ok {
		t.Fatalf("missing hy4-preview: %v", ids)
	}
	if _, ok := ids["glm-5.3"]; !ok {
		t.Fatalf("missing glm-5.3: %v", ids)
	}
	if !strings.Contains(got.Message, "Partial console host errors") {
		t.Fatalf("want partial host errors in Message, got %q", got.Message)
	}
	if !strings.Contains(got.Message, "www.codebuddy.ai") || !strings.Contains(got.Message, "HTTP 500") {
		t.Fatalf("want failed host + status in Message, got %q", got.Message)
	}
}

func listConsoleViaHosts(t *testing.T, primary, secondary *httptest.Server) ListResult {
	t.Helper()
	client := &provider.Client{
		HTTP: &http.Client{Transport: rewriteHostsTransport{
			hosts: map[string]*httptest.Server{
				"www.codebuddy.ai":    primary,
				"copilot.tencent.com": secondary,
			},
		}},
	}
	return NewLister().List(context.Background(), client, ListOptions{
		BearerToken: "test-token",
		Site:        "global",
		BaseURL:     "https://www.codebuddy.ai",
	})
}

func modelIDSet(got ListResult) map[string]struct{} {
	ids := make(map[string]struct{}, len(got.Models))
	for _, m := range got.Models {
		ids[m.ID] = struct{}{}
	}
	return ids
}

func consoleCatalogHandler(status int, ids ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != consoleModelsPath {
			http.NotFound(w, r)
			return
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		rows := make([]map[string]any, 0, len(ids))
		for _, id := range ids {
			rows = append(rows, map[string]any{"id": id, "name": id})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"models": rows})
	}
}

type rewriteHostsTransport struct {
	hosts map[string]*httptest.Server
}

func (t rewriteHostsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if srv, ok := t.hosts[req.URL.Host]; ok {
		target, err := url.Parse(srv.URL)
		if err != nil {
			return nil, err
		}
		clone.URL.Scheme = target.Scheme
		clone.URL.Host = target.Host
		clone.Host = target.Host
	} else {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("no mock for " + req.URL.Host)),
			Header:     make(http.Header),
			Request:    clone,
		}, nil
	}
	return http.DefaultTransport.RoundTrip(clone)
}
