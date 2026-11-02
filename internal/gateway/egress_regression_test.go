package gateway

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/oauth"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

type egressDirectTrap struct{ calls atomic.Int64 }

func (t *egressDirectTrap) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return &http.Response{StatusCode: 200, Header: http.Header{},
		Body: io.NopCloser(strings.NewReader(`{"code":0,"models":[{"id":"direct-model"}],"results":[]}`)), Request: req}, nil
}

func TestAllCredentialedPathsHonorBoundProxy(t *testing.T) {
	svc := New(config.Config{Site: "domestic", AccountsPath: t.TempDir() + "/accounts.json"}, nil)
	defer svc.Close()
	account, _, err := svc.Pool.Upsert(accounts.Account{
		Site: "domestic", Enabled: true, BearerToken: "synthetic-token",
		APIEndpoint: "http://upstream.invalid", Proxy: "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	trap := &egressDirectTrap{}
	svc.Provider.HTTP = &http.Client{Transport: trap}
	opts := SearchOptions{AccountID: account.ID, FixedAccount: true}
	if _, _, err := svc.WebSearchFromPool(t.Context(), opts, provider.WebSearchRequest{Query: "test"}); err == nil {
		t.Fatal("search succeeded despite unreachable bound proxy")
	}
	if _, _, err := svc.WebFetchFromPool(t.Context(), opts, provider.WebFetchRequest{URL: "https://example.invalid"}); err == nil {
		t.Fatal("web fetch succeeded despite unreachable bound proxy")
	}
	_, _ = svc.ListModels(t.Context(), true)
	if got := trap.calls.Load(); got != 0 {
		t.Fatalf("credentialed requests bypassed binding: direct calls=%d", got)
	}
}

func TestRefreshPreservesConcurrentProxyAssignment(t *testing.T) {
	svc := New(config.Config{Site: "global", AccountsPath: t.TempDir() + "/accounts.json"}, nil)
	defer svc.Close()
	previous, _, err := svc.Pool.Upsert(accounts.Account{
		Site: "global", Enabled: true, BearerToken: "old-token", RefreshToken: "old-refresh",
	})
	if err != nil {
		t.Fatal(err)
	}
	const proxy = "socks5h://127.0.0.1:1080"
	if _, _, err := svc.Pool.SetProxy(previous.ID, proxy); err != nil {
		t.Fatal(err)
	}
	updated, _, err := svc.SaveRefreshedToken(previous, &oauth.TokenData{
		BearerToken: "new-token", RefreshToken: "new-refresh", ExpiresIn: 3600,
	})
	if err != nil || updated.Proxy != proxy || updated.BearerToken != "new-token" {
		t.Fatalf("refresh lost concurrent proxy: proxy=%q err=%v", updated.Proxy, err)
	}
	// A late refresh response must also not overwrite credentials imported in the meantime.
	again, _, err := svc.SaveRefreshedToken(previous, &oauth.TokenData{BearerToken: "stale-token"})
	if err != nil || again.BearerToken != "new-token" {
		t.Fatalf("late refresh overwrote newer credentials: err=%v", err)
	}
}
