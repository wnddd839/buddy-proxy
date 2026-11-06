package models

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

func TestConsoleNegativeCacheSkipsRecentlyFailedHost(t *testing.T) {
	var mu sync.Mutex
	primaryHits, secondaryHits := 0, 0
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == consoleModelsPath {
			mu.Lock()
			primaryHits++
			mu.Unlock()
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer primary.Close()
	handler := consoleCatalogHandler(http.StatusOK, "hy4-preview")
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == consoleModelsPath {
			mu.Lock()
			secondaryHits++
			mu.Unlock()
		}
		handler(w, r)
	}))
	defer secondary.Close()

	client := &provider.Client{HTTP: &http.Client{Transport: rewriteHostsTransport{
		hosts: map[string]*httptest.Server{
			"www.codebuddy.ai":    primary,
			"copilot.tencent.com": secondary,
		},
	}}}
	lister := NewLister()
	opts := ListOptions{BearerToken: "test-token", Site: "global", BaseURL: "https://www.codebuddy.ai"}
	for _, call := range []string{"first", "second"} {
		if got := lister.List(t.Context(), client, opts); got.ModelsSource != "console" {
			t.Fatalf("%s modelsSource=%q want console; message=%q", call, got.ModelsSource, got.Message)
		}
	}
	mu.Lock()
	p1, s2 := primaryHits, secondaryHits
	mu.Unlock()
	if p1 != 1 {
		t.Fatalf("failed host hit %d times, want 1 (negative cache)", p1)
	}
	if s2 != 2 {
		t.Fatalf("healthy host hit %d times, want 2", s2)
	}

	fresh := opts
	fresh.Fresh = true
	if got := lister.List(t.Context(), client, fresh); got.ModelsSource != "console" {
		t.Fatalf("fresh modelsSource=%q want console", got.ModelsSource)
	}
	mu.Lock()
	p2 := primaryHits
	mu.Unlock()
	if p2 != 2 {
		t.Fatalf("fresh must retry failed host, primaryHits=%d want 2", p2)
	}
}
