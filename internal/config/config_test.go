package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

func TestLoadUsagePathDefault(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	accounts := filepath.Join(dir, "proxy-accounts.json")
	t.Setenv("CODEBUDDY_PROXY_ACCOUNTS_PATH", accounts)
	t.Setenv("CODEBUDDY_PROXY_USAGE_PATH", "")

	cfg := config.Load()
	want := filepath.Join(dir, "proxy-usage.json")
	if cfg.UsagePath != want {
		t.Fatalf("UsagePath=%q want %q", cfg.UsagePath, want)
	}
	if cfg.UsagePath == cfg.AccountsPath {
		t.Fatal("usage path must not equal accounts path")
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("CODEBUDDY_PROXY_HOST", "")
	t.Setenv("CURSOR_DIRECT_HOST", "")
	t.Setenv("CODEBUDDY_PROXY_PORT", "")
	t.Setenv("CURSOR_DIRECT_PORT", "")
	t.Setenv("CODEBUDDY_PROXY_API_KEY", "")
	t.Setenv("CODEBUDDY_PROXY_API_KEYS", "")
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

func TestDefaultUsagePath(t *testing.T) {
	accounts := filepath.Join("/data", "pool", "proxy-accounts.json")
	want := filepath.Join("/data", "pool", "proxy-usage.json")
	if got := config.DefaultUsagePath(accounts); got != want {
		t.Fatalf("DefaultUsagePath=%q want %q", got, want)
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

func TestOptionalSiteEmptyStaysEmpty(t *testing.T) {
	if got := config.OptionalSite(""); got != "" {
		t.Fatalf("OptionalSite empty=%q", got)
	}
	if got := config.OptionalSite("cn"); got != "domestic" {
		t.Fatalf("OptionalSite cn=%q", got)
	}
}

func TestLoadAPIKeys(t *testing.T) {
	t.Setenv("CODEBUDDY_PROXY_API_KEY", "")
	t.Setenv("CURSOR_DIRECT_API_KEY", "")
	t.Setenv("CURSOR_GATEWAY_API_KEY", "")
	t.Setenv("CODEBUDDY_PROXY_API_KEYS", "cbp_aaa:global, cbp_bbb:cn")
	t.Setenv("CODEBUDDY_PROXY_REQUIRE_API_KEY", "")
	cfg := config.Load()
	if !cfg.RequireAPIKey {
		t.Fatal("mapped keys should enable requireApiKey")
	}
	if len(cfg.APIKeys) != 2 || cfg.APIKeys[0].Site != "global" || cfg.APIKeys[1].Site != "domestic" {
		t.Fatalf("APIKeys=%#v", cfg.APIKeys)
	}
}

func TestParseAPIKeys(t *testing.T) {
	got := config.ParseAPIKeys("cbp_aaa:global, cbp_bbb:cn,cbp_ccc")
	want := []config.APIKeyBinding{
		{Key: "cbp_aaa", Site: "global"},
		{Key: "cbp_bbb", Site: "domestic"},
		{Key: "cbp_ccc", Site: ""},
	}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("item %d = %#v want %#v", i, got[i], want[i])
		}
	}
	if n := len(config.ParseAPIKeys("")); n != 0 {
		t.Fatalf("empty input len=%d", n)
	}
}

func TestLookupAPIKey(t *testing.T) {
	cfg := config.Config{
		APIKey:        "cbp_primary",
		RequireAPIKey: true,
		APIKeys:       config.ParseAPIKeys("cbp_aaa:global,cbp_bbb:domestic"),
	}
	cases := []struct {
		token string
		ok    bool
		site  string
	}{
		{"cbp_aaa", true, "global"},
		{"cbp_bbb", true, "domestic"},
		{"cbp_primary", true, ""},
		{"cbp_unknown", false, ""},
		{"", false, ""},
	}
	for _, tc := range cases {
		ok, site := cfg.LookupAPIKey(tc.token)
		if ok != tc.ok || site != tc.site {
			t.Fatalf("LookupAPIKey(%q)=(%v,%q) want (%v,%q)", tc.token, ok, site, tc.ok, tc.site)
		}
	}
}

func TestLookupAPIKeyPrimaryAlsoBound(t *testing.T) {
	cfg := config.Config{
		APIKey:  "cbp_aaa",
		APIKeys: config.ParseAPIKeys("cbp_aaa:cn"),
	}
	ok, site := cfg.LookupAPIKey("cbp_aaa")
	if !ok || site != "domestic" {
		t.Fatalf("bound primary = (%v,%q) want (true, domestic)", ok, site)
	}
}

func TestSplitSitePrefix(t *testing.T) {
	tests := []struct {
		in   string
		site string
		rest string
		ok   bool
	}{
		{"deepseek-v4", "", "deepseek-v4", false},
		{"cn:deepseek-v4.1-flash", "domestic", "deepseek-v4.1-flash", true},
		{"global:auto", "global", "auto", true},
		{"domestic:glm-5.3-flash", "domestic", "glm-5.3-flash", true},
		{"intl:gpt-5", "global", "gpt-5", true},
		{"codebuddy:deepseek-v4", "", "codebuddy:deepseek-v4", false},
		{"cn:", "domestic", "auto", true},
	}
	for _, tc := range tests {
		site, rest, ok := config.SplitSitePrefix(tc.in)
		if site != tc.site || rest != tc.rest || ok != tc.ok {
			t.Fatalf("SplitSitePrefix(%q)=(%q,%q,%v) want (%q,%q,%v)", tc.in, site, rest, ok, tc.site, tc.rest, tc.ok)
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
