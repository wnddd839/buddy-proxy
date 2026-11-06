package gateway

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

type eofOnceTransport struct {
	inner http.RoundTripper

	mu       sync.Mutex
	attempts int
	failed   bool
}

func (t *eofOnceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Path, "chat/completions") {
		t.mu.Lock()
		t.attempts++
		fail := !t.failed
		t.failed = true
		t.mu.Unlock()
		if fail {
			return nil, io.ErrUnexpectedEOF
		}
	}
	return t.inner.RoundTrip(req)
}

func (t *eofOnceTransport) chatAttempts() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.attempts
}

func newTransportRetryService(t *testing.T) *Service {
	t.Helper()
	svc := New(config.Config{Site: "global", AccountsPath: filepath.Join(t.TempDir(), "accounts.json")},
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func TestCompleteFromPoolRetriesSameAccountAfterTransportEOF(t *testing.T) {
	svc := newTransportRetryService(t)
	only, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "only", Site: "global", BearerToken: "token-global", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	scripted := &scriptedChatTransport{}
	transport := &eofOnceTransport{inner: scripted}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	result, err := svc.CompleteFromPool(t.Context(), CompleteOptions{
		Model: "auto", Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatalf("单账号重连应吃掉一次 transport EOF: %v", err)
	}
	if got := transport.chatAttempts(); got != 2 {
		t.Fatalf("chat attempts=%d want 2", got)
	}
	if result.AccountID != only.ID {
		t.Fatalf("AccountID=%s want %s", result.AccountID, only.ID)
	}
	if result.Turn.Text != "ok" {
		t.Fatalf("text=%q want ok", result.Turn.Text)
	}
}

func TestCompleteFromPoolSwitchesAccountWhenAlternativeExists(t *testing.T) {
	svc := newTransportRetryService(t)
	if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "first", Site: "global", BearerToken: "token-first", Enabled: true,
	})); err != nil {
		t.Fatal(err)
	}
	second, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "second", Site: "global", BearerToken: "token-second", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	scripted := &scriptedChatTransport{}
	svc.Provider.HTTP = &http.Client{Transport: &eofOnceTransport{inner: scripted}}

	result, err := svc.CompleteFromPool(t.Context(), CompleteOptions{
		Model: "auto", Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatalf("有备用账号时应换号重试: %v", err)
	}
	if result.AccountID != second.ID {
		t.Fatalf("AccountID=%s want %s", result.AccountID, second.ID)
	}
}

func TestCompleteFromPoolStopsSameAccountRetryAfterBudget(t *testing.T) {
	svc := newTransportRetryService(t)
	if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "only", Site: "global", BearerToken: "token-global", Enabled: true,
	})); err != nil {
		t.Fatal(err)
	}
	svc.Provider.HTTP = &http.Client{Transport: roundTripAlwaysEOF{}}
	_, err := svc.CompleteFromPool(t.Context(), CompleteOptions{
		Model: "auto", Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err == nil {
		t.Fatal("持续 EOF 必须最终失败")
	}
	if !strings.Contains(err.Error(), "EOF") {
		t.Fatalf("应保留原始传输错误，实际=%v", err)
	}
}

type roundTripAlwaysEOF struct{}

func (roundTripAlwaysEOF) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, io.ErrUnexpectedEOF
}

