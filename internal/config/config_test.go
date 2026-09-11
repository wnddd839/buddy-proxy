package config_test

import (
	"os"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("CODEBUDDY_PROXY_HOST", "")
	t.Setenv("CURSOR_DIRECT_HOST", "")
	t.Setenv("CODEBUDDY_PROXY_PORT", "")
	t.Setenv("CURSOR_DIRECT_PORT", "")
	t.Setenv("CODEBUDDY_PROXY_API_KEY", "")
	t.Setenv("CURSOR_DIRECT_API_KEY", "")
	t.Setenv("CURSOR_GATEWAY_API_KEY", "")
	_ = os.Unsetenv("CODEBUDDY_PROXY_HOST")
	_ = os.Unsetenv("CURSOR_DIRECT_HOST")

	cfg := config.Load()
	if cfg.Host != "127.0.0.1" {
		t.Fatalf("host=%q", cfg.Host)
	}
	if cfg.Port != 32126 {
		t.Fatalf("port=%d", cfg.Port)
	}
	if cfg.Transport != "protocol_direct" {
		t.Fatalf("transport=%q", cfg.Transport)
	}
	if cfg.ChatCompletionsPath != "/v2/chat/completions" {
		t.Fatalf("path=%q", cfg.ChatCompletionsPath)
	}
}

func TestNormalizeSite(t *testing.T) {
	cases := map[string]string{
		"":              "global",
		"global":        "global",
		"international": "global",
		"domestic":      "domestic",
		"CN":            "domestic",
		"china":         "domestic",
		"internal":      "domestic",
	}
	for in, want := range cases {
		if got := config.NormalizeSite(in); got != want {
			t.Fatalf("NormalizeSite(%q)=%q want %q", in, got, want)
		}
	}
}

func TestNormalizeProduct(t *testing.T) {
	cases := map[string]string{
		"":          "codebuddy",
		"codebuddy": "codebuddy",
		"CodeBuddy": "codebuddy",
		"cli":       "codebuddy",
		"workbuddy": "workbuddy",
		"WorkBuddy": "workbuddy",
		"wb":        "workbuddy",
		"ide":       "workbuddy",
	}
	for in, want := range cases {
		if got := config.NormalizeProduct(in); got != want {
			t.Fatalf("NormalizeProduct(%q)=%q want %q", in, got, want)
		}
	}
}

func TestProductPortalBaseURL(t *testing.T) {
	cases := []struct {
		site, product, want string
	}{
		{"domestic", "codebuddy", "https://www.codebuddy.cn"},
		{"global", "codebuddy", "https://www.codebuddy.ai"},
		{"domestic", "workbuddy", "https://www.workbuddy.cn"},
		{"global", "workbuddy", "https://www.workbuddy.ai"},
		{"cn", "wb", "https://www.workbuddy.cn"},
	}
	for _, tc := range cases {
		got := config.ProductPortalBaseURL(tc.site, tc.product)
		if got != tc.want {
			t.Fatalf("ProductPortalBaseURL(%q,%q)=%q want %q", tc.site, tc.product, got, tc.want)
		}
	}
}
