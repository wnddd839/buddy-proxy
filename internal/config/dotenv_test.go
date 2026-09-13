package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

func TestUpsertAndLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("CODEBUDDY_PROXY_HOST=127.0.0.1\r\n# comment\nEXISTING=keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.UpsertEnvFile(envPath, map[string]string{
		"CODEBUDDY_PROXY_API_KEY":         "cbp_testkey",
		"CODEBUDDY_PROXY_REQUIRE_API_KEY": "true",
		"EXISTING":                        "updated",
	}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !containsAll(text, "CODEBUDDY_PROXY_API_KEY=cbp_testkey", "EXISTING=updated", "# comment") {
		t.Fatalf("unexpected env content:\n%s", text)
	}

	t.Setenv("CODEBUDDY_PROXY_ENV_FILE", envPath)
	_ = os.Unsetenv("CODEBUDDY_PROXY_API_KEY")
	_ = os.Unsetenv("CODEBUDDY_PROXY_REQUIRE_API_KEY")
	_ = os.Unsetenv("EXISTING")

	loaded, err := config.LoadDotEnv()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) == 0 {
		t.Fatal("expected loaded env file")
	}
	if got := os.Getenv("CODEBUDDY_PROXY_API_KEY"); got != "cbp_testkey" {
		t.Fatalf("api key=%q", got)
	}
	if got := os.Getenv("EXISTING"); got != "updated" {
		t.Fatalf("existing=%q", got)
	}
}

func TestResolveEnvFilePathDefaultsToStableHomeFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "launch")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("CODEBUDDY_PROXY_ENV_FILE", "")
	_ = os.Unsetenv("CODEBUDDY_PROXY_ENV_FILE")

	got := config.ResolveEnvFilePath()
	want := config.DefaultEnvFilePath()
	if got != want {
		t.Fatalf("ResolveEnvFilePath=%q want stable %q", got, want)
	}
}

func TestLoadDotEnvFillsEmptyProcessAPIKey(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "proxy.env")
	if err := os.WriteFile(envPath, []byte("CODEBUDDY_PROXY_API_KEY=cbp_persisted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEBUDDY_PROXY_ENV_FILE", envPath)
	t.Setenv("CODEBUDDY_PROXY_API_KEY", "")

	if _, err := config.LoadDotEnv(); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("CODEBUDDY_PROXY_API_KEY"); got != "cbp_persisted" {
		t.Fatalf("api key=%q want cbp_persisted (empty process env must not block .env)", got)
	}
}

func TestDefaultEnvFilePathBesideAccounts(t *testing.T) {
	got := config.DefaultEnvFilePath()
	if filepath.Base(got) != "proxy.env" {
		t.Fatalf("base=%q want proxy.env", filepath.Base(got))
	}
	if filepath.Base(filepath.Dir(got)) != ".codebuddy" && !stringsHas(filepath.Dir(got), ".codebuddy") {
		t.Fatalf("dir=%q should be under .codebuddy", filepath.Dir(got))
	}
}

func stringsHas(s, sub string) bool {
	return stringIndex(s, sub) >= 0
}

func containsAll(text string, parts ...string) bool {
	for _, p := range parts {
		if !contains(text, p) {
			return false
		}
	}
	return true
}

func contains(text, part string) bool {
	return len(text) >= len(part) && (text == part || len(part) == 0 || stringIndex(text, part) >= 0)
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
