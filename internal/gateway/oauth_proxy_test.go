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

func TestOAuthProxyCoversAllGlobalCalls(t *testing.T) {
	svc := newOAuthProxyService(t, config.Config{Site: "global", OAuthProxy: "socks5h://127.0.0.1:17998"})
	if svc.oauthGlobal == nil {
		t.Fatal("valid proxy must create the dedicated client")
	}
	if got := svc.oauthFor("global"); got != svc.oauthGlobal {
		t.Fatal("Start/Poll must use the dedicated client")
	}
	if got := svc.OAuthHTTPFor("global"); got != svc.oauthGlobal.HTTP {
		t.Fatal("token refresh must use the dedicated client")
	}
	if got := svc.oauthFor("domestic"); got != svc.OAuth {
		t.Fatal("domestic must keep the default client")
	}
	if got := svc.OAuthHTTPFor("domestic"); got != nil {
		t.Fatal("domestic refresh must keep the account egress")
	}
}

func TestOAuthProxyInvalidFallsBackToDefault(t *testing.T) {
	svc := newOAuthProxyService(t, config.Config{Site: "global", OAuthProxy: "ftp://127.0.0.1:17998"})
	if svc.oauthGlobal != nil {
		t.Fatal("invalid proxy must not create a fail-closed global OAuth client")
	}
	if got := svc.oauthFor("global"); got != svc.OAuth {
		t.Fatal("invalid proxy must fall back to the default client")
	}
}

func TestOAuthProxyUnsetFallsBack(t *testing.T) {
	svc := newOAuthProxyService(t, config.Config{Site: "domestic"})
	if svc.oauthGlobal != nil || svc.OAuthHTTPFor("global") != nil {
		t.Fatal("no proxy configured must stay on default client")
	}
}
