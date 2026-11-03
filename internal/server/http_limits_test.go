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
	t.Chdir(t.TempDir())
	for _, tc := range []struct {
		body, idle string
		wantMB     int64
		wantIdle   time.Duration
	}{
		{"", "", 64, 120 * time.Second},
		{"0", "0", 64, 120 * time.Second},
		{"-1", "-1", 64, 120 * time.Second},
		{"invalid", "invalid", 64, 120 * time.Second},
		{"128", "5000", 128, 5 * time.Second},
	} {
		t.Setenv("CODEBUDDY_PROXY_MAX_BODY_MB", tc.body)
		t.Setenv("CODEBUDDY_PROXY_SERVER_IDLE_TIMEOUT_MS", tc.idle)
		cfg := config.Load()
		s := testServerCfg(t, config.Config{MaxBodyMB: cfg.MaxBodyMB, ServerIdleTimeout: cfg.ServerIdleTimeout})
		if s.HTTP.IdleTimeout != tc.wantIdle || s.maxJSONBodyBytes != tc.wantMB<<20 {
			t.Fatalf("body=%d idle=%s; want %d MiB / %s", s.maxJSONBodyBytes, s.HTTP.IdleTimeout, tc.wantMB, tc.wantIdle)
		}
	}
}
