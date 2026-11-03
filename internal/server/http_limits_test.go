package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

func TestPublicJSONLimitOverHTTP(t *testing.T) {
	paths := []string{"/v1/chat/completions", "/v1/responses", "/v1/images/generations", "/v1/search", "/v1/webfetch", "/v1/agentic_search"}
	for _, limit := range []int{1, 2} {
		s := testServerCfg(t, config.Config{MaxBodyMB: limit, Site: "domestic"})
		seedBothSites(t, s)
		s.Svc.Provider.HTTP = &http.Client{Transport: &responsesUpstreamTransport{}}
		srv := httptest.NewServer(s.HTTP.Handler)
		t.Cleanup(srv.Close)
		for _, path := range paths {
			body := strings.Repeat(" ", 1<<20) + `{"model":"auto","messages":[{"role":"user","content":"hi"}],"input":"hi"}`
			resp, err := srv.Client().Post(srv.URL+path, "application/json", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if limit == 1 {
				if resp.StatusCode != http.StatusRequestEntityTooLarge || !strings.Contains(string(raw), "1MB") {
					t.Fatalf("%s: status=%d body=%s", path, resp.StatusCode, raw)
				}
			} else if resp.StatusCode == http.StatusRequestEntityTooLarge {
				t.Fatalf("%s: larger limit ignored: %s", path, raw)
			} else if path == "/v1/chat/completions" && resp.StatusCode != http.StatusOK {
				t.Fatalf("chat did not reach upstream: status=%d body=%s", resp.StatusCode, raw)
			}
		}
	}
}

func TestHTTPConfigDefaultsAndIdleTimeout(t *testing.T) {
	for _, tc := range []struct {
		bodyMB int
		idle   time.Duration
		want   time.Duration
	}{
		{0, 0, 120 * time.Second},
		{-1, -1, 120 * time.Second},
		{128, 5 * time.Second, 5 * time.Second},
	} {
		s := testServerCfg(t, config.Config{MaxBodyMB: tc.bodyMB, ServerIdleTimeout: tc.idle})
		if s.HTTP.IdleTimeout != tc.want {
			t.Fatalf("idle timeout=%s want %s", s.HTTP.IdleTimeout, tc.want)
		}
		wantMB := tc.bodyMB
		if wantMB <= 0 {
			wantMB = 64
		}
		if s.maxJSONBodyBytes != int64(wantMB)<<20 {
			t.Fatalf("body limit=%d want %d MiB", s.maxJSONBodyBytes, wantMB)
		}
	}
}
