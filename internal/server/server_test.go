package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/gateway"
)

func testServer(t *testing.T, requireAPIKey bool, adminPassword, apiKey string) *Server {
	t.Helper()
	cfg := config.Config{
		Host:          "127.0.0.1",
		Port:          32126,
		RequireAPIKey: requireAPIKey,
		APIKey:        apiKey,
		AdminPassword: adminPassword,
		AccountsPath:  filepath.Join(t.TempDir(), "accounts.json"),
		Site:          "domestic",
		Transport:     config.DefaultTransport,
	}
	svc := gateway.New(cfg, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	return New(cfg, svc)
}

func TestAuthorizeAPIKeyRequired(t *testing.T) {
	srv := testServer(t, true, "", "secret-key")
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126/v1/models", nil)
	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126/v1/models", nil)
	req.Header.Set("Authorization", "Bearer secret-key")
	rec = httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("with key status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestModelInfoAliasRoute(t *testing.T) {
	srv := testServer(t, true, "", "secret-key")
	for _, path := range []string{"/v1/model/info", "/model/info"} {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126"+path, nil)
		req.Header.Set("Authorization", "Bearer secret-key")
		rec := httptest.NewRecorder()
		srv.HTTP.Handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Fatalf("path %s returned 404", path)
		}
		if rec.Code != http.StatusOK && rec.Code != http.StatusBadGateway {
			t.Fatalf("path %s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestAdminOpenWithoutPassword(t *testing.T) {
	srv := testServer(t, true, "", "secret-key")
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126/direct-admin/", nil)
	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin status=%d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "CodeBuddy") {
		t.Fatalf("unexpected admin body")
	}
}

func TestAdminCheckinRouteUsesActivePoolSiteByDefault(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantSite string
	}{
		{"empty body falls back to active pool", `{}`, "domestic"},
		{"explicit global overrides", `{"site":"global"}`, "global"},
		{"alias normalizes to domestic", `{"site":"cn"}`, "domestic"},
		{"invalid json falls back to active pool", `not-json`, "domestic"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := testServer(t, false, "", "")
			req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32126/direct-admin/api/codebuddy/checkin", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "http://127.0.0.1:32126")
			req.Host = "127.0.0.1:32126"
			rec := httptest.NewRecorder()
			srv.HTTP.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var payload struct {
				OK       bool   `json:"ok"`
				PoolSite string `json:"poolSite"`
				Summary  struct {
					Total int `json:"total"`
				} `json:"summary"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.PoolSite != tc.wantSite {
				t.Fatalf("poolSite=%q want=%q", payload.PoolSite, tc.wantSite)
			}
			// Empty pool: no upstream calls, but the batch must still report cleanly.
			if !payload.OK || payload.Summary.Total != 0 {
				t.Fatalf("payload=%s", rec.Body.String())
			}
		})
	}
}

func TestAdminCSRFBlocksCrossOriginMutation(t *testing.T) {
	srv := testServer(t, false, "", "")
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32126/direct-admin/api/pool-site", strings.NewReader(`{"site":"global"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	req.Host = "127.0.0.1:32126"
	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("csrf status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != false {
		t.Fatalf("payload=%v", payload)
	}
}

func TestAdminCSRFAllowsSameOriginMutation(t *testing.T) {
	srv := testServer(t, false, "", "")
	t.Setenv("CODEBUDDY_PROXY_ENV_FILE", filepath.Join(t.TempDir(), ".env"))
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32126/direct-admin/api/pool-site", strings.NewReader(`{"site":"domestic"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:32126")
	req.Host = "127.0.0.1:32126"
	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("same-origin status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminProductSwitchSameOrigin(t *testing.T) {
	srv := testServer(t, false, "", "")
	t.Setenv("CODEBUDDY_PROXY_ENV_FILE", filepath.Join(t.TempDir(), ".env"))
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32126/direct-admin/api/pool-product", strings.NewReader(`{"product":"workbuddy"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:32126")
	req.Host = "127.0.0.1:32126"
	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("product switch status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != true {
		t.Fatalf("payload=%v", payload)
	}
	product, _ := payload["product"].(string)
	poolProduct, _ := payload["poolProduct"].(string)
	if product != "workbuddy" && poolProduct != "workbuddy" {
		t.Fatalf("payload product missing: %v", payload)
	}
}

func TestAdminPasswordRequiredWhenConfigured(t *testing.T) {
	srv := testServer(t, false, "admin-pass", "")
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126/direct-admin/api/status", nil)
	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126/direct-admin/api/status", nil)
	req.SetBasicAuth("admin", "admin-pass")
	rec = httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authed status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Bearer admin password also works.
	req = httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126/direct-admin/api/status", nil)
	req.Header.Set("Authorization", "Bearer admin-pass")
	rec = httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminRejectsPasswordQueryParam(t *testing.T) {
	srv := testServer(t, false, "admin-pass", "")
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126/direct-admin/api/status?password=admin-pass", nil)
	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("query password must be rejected, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHealth(t *testing.T) {
	srv := testServer(t, false, "", "")
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126/health", nil)
	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health=%d", rec.Code)
	}
}
