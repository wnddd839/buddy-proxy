package gateway

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

func newOAuthProxyService(t *testing.T, cfg config.Config) *Service {
	t.Helper()
	if cfg.AccountsPath == "" {
		cfg.AccountsPath = filepath.Join(t.TempDir(), "accounts.json")
	}
	svc := New(cfg, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func TestOAuthProxySelectsGlobalClient(t *testing.T) {
	svc := newOAuthProxyService(t, config.Config{Site: "global", OAuthProxy: "socks5h://127.0.0.1:17998"})
	if svc.oauthGlobal == nil {
		t.Fatal("oauthGlobal must be created when OAuthProxy is set")
	}
	if got := svc.oauthFor("global"); got != svc.oauthGlobal {
		t.Fatal("global site must use the dedicated OAuth client")
	}
	if got := svc.oauthFor("domestic"); got != svc.OAuth {
		t.Fatal("domestic site must keep the default OAuth client")
	}
}

func TestOAuthProxyUnsetFallsBack(t *testing.T) {
	svc := newOAuthProxyService(t, config.Config{Site: "domestic"})
	if svc.oauthGlobal != nil {
		t.Fatal("oauthGlobal must stay nil when OAuthProxy is empty")
	}
	if got := svc.oauthFor("global"); got != svc.OAuth {
		t.Fatal("global site must fall back to the default client when no proxy is configured")
	}
}

func TestOAuthProxyLabelMasksCredentials(t *testing.T) {
	svc := newOAuthProxyService(t, config.Config{
		Site:       "global",
		OAuthProxy: "socks5h://user:secret@127.0.0.1:17998",
	})
	if got, want := svc.OAuthProxyLabel(), "127.0.0.1:17998"; got != want {
		t.Fatalf("OAuthProxyLabel()=%q want %q (credentials must not leak)", got, want)
	}

	plain := newOAuthProxyService(t, config.Config{Site: "domestic"})
	if got := plain.OAuthProxyLabel(); got != "" {
		t.Fatalf("OAuthProxyLabel() without proxy=%q want empty", got)
	}
}
