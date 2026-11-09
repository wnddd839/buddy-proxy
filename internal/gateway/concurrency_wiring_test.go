package gateway

import (
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

func newConcurrencyService(t *testing.T) *Service {
	t.Helper()
	svc := New(config.Config{Site: "domestic", AccountsPath: filepath.Join(t.TempDir(), "accounts.json")},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { _ = svc.Close() })
	if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Site: "domestic", BearerToken: "a", Enabled: true,
	})); err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestConcurrencyAllUpstreamPathsEndFlight(t *testing.T) {
	svc := newConcurrencyService(t)
	svc.Provider.HTTP = &http.Client{Transport: &scriptedChatTransport{}}
	_, _ = svc.CompleteFromPool(t.Context(), CompleteOptions{
		Model: "auto", Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	_, _ = svc.ImageFromPool(t.Context(), ImageOptions{Model: "image", Prompt: "p"})
	if got := svc.ConcurrencyStatus().GlobalInFlight; got != 0 {
		t.Fatalf("in-flight leak=%d", got)
	}
	statusSummary := svc.Status()["concurrency"].(map[string]any)
	if got := statusSummary["accounts"].(int); got == 0 {
		t.Fatal("status summary must count observed accounts")
	}
	// The admin console paints the status payload on every 15s poll. The
	// concurrency field is documented as a numeric summary, so a snapshot
	// array here would throw in paintConcurrency and leave the whole account
	// pool panel empty.
	if _, ok := statusSummary["accounts"].(int); !ok {
		t.Fatal("status concurrency.accounts must be a number")
	}
	if _, ok := statusSummary["rateLimits"].(int); !ok {
		t.Fatal("status concurrency.rateLimits must be a number")
	}
	if _, ok := statusSummary["globalInFlight"].(int); !ok {
		t.Fatal("status concurrency.globalInFlight must be a number")
	}
}

func TestConcurrencyRateLimitRecordedBeforeEndFlight(t *testing.T) {
	svc := newConcurrencyService(t)
	svc.Provider.HTTP = &http.Client{Transport: &scriptedChatTransport{
		byAuth:          map[string]int{"a": http.StatusTooManyRequests},
		errorBodyByAuth: map[string]string{"a": `{"error":"429 too many requests"}`},
	}}
	_, _ = svc.CompleteFromPool(t.Context(), CompleteOptions{
		Model: "auto", Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	events := svc.ConcurrencyStatus().RateLimits
	if len(events) == 0 || events[len(events)-1].AccountInFlight < 1 {
		t.Fatalf("rate limit must be recorded while the request is in flight: %+v", events)
	}
}
