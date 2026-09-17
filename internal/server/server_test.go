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
	"sync"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/gateway"
)

func testServer(t *testing.T, requireAPIKey bool, adminPassword, apiKey string) *Server {
	t.Helper()
	return testServerCfg(t, config.Config{
		Host:          "127.0.0.1",
		Port:          32126,
		RequireAPIKey: requireAPIKey,
		APIKey:        apiKey,
		AdminPassword: adminPassword,
		AccountsPath:  filepath.Join(t.TempDir(), "accounts.json"),
		Site:          "domestic",
		Transport:     config.DefaultTransport,
	})
}

func testServerCfg(t *testing.T, cfg config.Config) *Server {
	t.Helper()
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	if cfg.Port == 0 {
		cfg.Port = 32126
	}
	if cfg.AccountsPath == "" {
		cfg.AccountsPath = filepath.Join(t.TempDir(), "accounts.json")
	}
	if cfg.Transport == "" {
		cfg.Transport = config.DefaultTransport
	}
	svc := gateway.New(cfg, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	svc.Provider.HTTP = &http.Client{Transport: stubProbeTransport{status: http.StatusUnauthorized, body: "Authorization Required"}}
	t.Cleanup(func() { _ = svc.Close() })
	return New(cfg, svc)
}

func TestResponsesAPIExplainsChatCompletionsOnly(t *testing.T) {
	srv := testServer(t, true, "", "secret-key")
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32126/v1/responses", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer secret-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Chat Completions") || !strings.Contains(rec.Body.String(), "/v1/chat/completions") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestResolveRequestSite(t *testing.T) {
	cases := []struct {
		model, header, key, want string
	}{
		{"global", "cn", "domestic", "global"},
		{"", "cn", "global", "domestic"},
		{"", "", "cn", "domestic"},
		{"", "", "", ""},
	}
	for _, tc := range cases {
		got := resolveRequestSite(tc.model, tc.header, tc.key)
		if got != tc.want {
			t.Fatalf("resolveRequestSite(%q,%q,%q)=%q want %q", tc.model, tc.header, tc.key, got, tc.want)
		}
	}
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

func TestAuthorizeMappedAPIKeys(t *testing.T) {
	srv := testServerCfg(t, config.Config{
		RequireAPIKey: true,
		APIKey:        "cbp_primary",
		APIKeys:       config.ParseAPIKeys("cbp_cn:domestic,cbp_global:global"),
		Site:          "domestic",
	})

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126/v1/models", nil)
	req.Header.Set("Authorization", "Bearer cbp_cn")
	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("mapped key status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126/v1/models", nil)
	req.Header.Set("Authorization", "Bearer cbp_unknown")
	rec = httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown key status=%d", rec.Code)
	}
}

func TestModelsCatalogFollowsXSite(t *testing.T) {
	srv := testServer(t, false, "", "")
	seedBothSites(t, srv)
	srv.Svc.Provider.HTTP = &http.Client{Transport: catalogByHostTransport{}}

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126/v1/models", nil)
	req.Header.Set("X-Site", "global")
	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	ids := modelIDsFromList(t, rec.Body.Bytes())
	if !containsID(ids, "gpt-5") || containsID(ids, "glm-5.3-flash") {
		t.Fatalf("X-Site=global catalog=%v", ids)
	}
	for _, id := range ids {
		if strings.Contains(id, ":") {
			t.Fatalf("aliased id %q in %v", id, ids)
		}
	}
}

func TestModelsCatalogFollowsAPIKeySite(t *testing.T) {
	srv := testServerCfg(t, config.Config{
		RequireAPIKey: true,
		APIKey:        "cbp_primary",
		APIKeys:       config.ParseAPIKeys("cbp_cn:domestic,cbp_global:global"),
		Site:          "global",
		Product:       "codebuddy",
	})
	seedBothSites(t, srv)
	srv.Svc.Provider.HTTP = &http.Client{Transport: catalogByHostTransport{}}

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126/v1/models", nil)
	req.Header.Set("Authorization", "Bearer cbp_cn")
	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	ids := modelIDsFromList(t, rec.Body.Bytes())
	if !containsID(ids, "glm-5.3-flash") || containsID(ids, "gpt-5") {
		t.Fatalf("domestic key catalog=%v", ids)
	}
}

func TestChatFollowsAPIKeySiteUnlessModelPrefix(t *testing.T) {
	srv := testServerCfg(t, config.Config{
		RequireAPIKey: true,
		APIKey:        "cbp_primary",
		APIKeys:       config.ParseAPIKeys("cbp_cn:domestic,cbp_global:global"),
		Site:          "domestic",
		Product:       "codebuddy",
	})
	seedBothSites(t, srv)
	transport := &recordingUpstreamTransport{}
	srv.Svc.Provider.HTTP = &http.Client{Transport: transport}

	post := func(key, model string) {
		t.Helper()
		body := `{"model":"` + model + `","messages":[{"role":"user","content":"hi"}]}`
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32126/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.HTTP.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("key=%s model=%s status=%d body=%s", key, model, rec.Code, rec.Body.String())
		}
	}

	post("cbp_global", "auto")
	post("cbp_global", "cn:auto")
	got := transport.chatTokens()
	if len(got) < 2 {
		t.Fatalf("chat tokens=%v", got)
	}
	if got[0] != "token-global" {
		t.Fatalf("unprefixed model with global key used %q want token-global", got[0])
	}
	if got[1] != "token-domestic" {
		t.Fatalf("cn: prefix must override key site, used %q want token-domestic", got[1])
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

func TestAdminUsageAPI(t *testing.T) {
	srv := testServer(t, false, "", "")
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126/direct-admin/api/usage?range=day&limit=5", nil)
	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != true {
		t.Fatalf("payload=%v", payload)
	}
	if _, ok := payload["summary"]; !ok {
		t.Fatalf("missing summary: %v", payload)
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

type stubProbeTransport struct {
	status int
	body   string
}

func (t stubProbeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	header := make(http.Header)
	header.Set("Content-Type", "text/plain")
	return &http.Response{
		StatusCode: t.status,
		Status:     http.StatusText(t.status),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(t.body)),
		Request:    req,
	}, nil
}

func TestReadyzAndDeepHealthFollowUpstream(t *testing.T) {
	srv := testServer(t, false, "", "")
	srv.Svc.Provider.HTTP = &http.Client{Transport: stubProbeTransport{status: http.StatusBadGateway, body: "<html>openresty"}}

	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126/health?deep=1", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("deep health=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("liveness health should stay 200, got %d", rec.Code)
	}
}

func TestReadyzOKWhenAuthLayerAnswers(t *testing.T) {
	srv := testServer(t, false, "", "")
	srv.Svc.Provider.HTTP = &http.Client{Transport: stubProbeTransport{status: http.StatusUnauthorized, body: "Authorization Required"}}
	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestResolveIncludeUsage(t *testing.T) {
	if !resolveIncludeUsage(nil) {
		t.Fatal("nil stream_options should default to include usage")
	}
	if !resolveIncludeUsage(&streamOptions{}) {
		t.Fatal("absent include_usage should default to true")
	}
	yes, no := true, false
	if !resolveIncludeUsage(&streamOptions{IncludeUsage: &yes}) {
		t.Fatal("explicit true")
	}
	if resolveIncludeUsage(&streamOptions{IncludeUsage: &no}) {
		t.Fatal("explicit false must skip usage trailer")
	}
}

func seedBothSites(t *testing.T, srv *Server) {
	t.Helper()
	if _, _, err := srv.Svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "global", Site: "global", BearerToken: "token-global", Enabled: true,
	})); err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.Svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "domestic", Site: "domestic", BearerToken: "token-domestic", Enabled: true,
	})); err != nil {
		t.Fatal(err)
	}
}

func modelIDsFromList(t *testing.T, raw []byte) []string {
	t.Helper()
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode models: %v body=%s", err, raw)
	}
	ids := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		ids = append(ids, item.ID)
	}
	return ids
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

type recordingUpstreamTransport struct {
	mu    sync.Mutex
	chats []string
}

func (t *recordingUpstreamTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	header := make(http.Header)
	token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
	if strings.Contains(req.URL.Path, "chat") {
		t.mu.Lock()
		t.chats = append(t.chats, token)
		t.mu.Unlock()
		header.Set("Content-Type", "text/event-stream")
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Header:     header,
			Body:       io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")),
			Request:    req,
		}, nil
	}
	header.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(`{"code":0,"data":{}}`)),
		Request:    req,
	}, nil
}

func (t *recordingUpstreamTransport) chatTokens() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string{}, t.chats...)
}

type catalogByHostTransport struct{}

func (catalogByHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
	ids := []string{"deepseek-v4.1-flash", "gpt-5"}
	if token == "token-domestic" || strings.Contains(req.URL.Host, ".cn") {
		ids = []string{"deepseek-v4.1-flash", "glm-5.3-flash"}
	}
	rows := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, map[string]any{"id": id, "name": id})
	}
	body, _ := json.Marshal(map[string]any{"models": rows})
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    req,
	}, nil
}
