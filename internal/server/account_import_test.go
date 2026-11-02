package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postImport(t *testing.T, srv *Server, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32126/direct-admin/api/codebuddy/accounts/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestAccountsImportAddsAndDedupes(t *testing.T) {
	srv := testServer(t, true, "", "secret-key")

	payload := `{"text":"[{\"uid\":\"u-1\",\"nickname\":\"198****9015\",\"domain\":\"www.codebuddy.cn\",\"access_token\":\"tok-a\",\"refresh_token\":\"rt-a\",\"expires_at\":1795951021000}]"}`
	rec, out := postImport(t, srv, payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if out["added"] != float64(1) || out["failed"] != float64(0) {
		t.Fatalf("unexpected counters: %+v", out)
	}
	results, _ := out["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results=%v", out["results"])
	}
	first, _ := results[0].(map[string]any)
	if first["status"] != "added" || first["site"] != "domestic" {
		t.Fatalf("unexpected first result: %+v", first)
	}

	// 同一份凭据再导一次应该是更新，而不是新增第二条。
	rec, out = postImport(t, srv, payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("second status=%d body=%s", rec.Code, rec.Body.String())
	}
	if out["added"] != float64(0) || out["updated"] != float64(1) {
		t.Fatalf("expected update on re-import, got %+v", out)
	}

	store, err := srv.Svc.Pool.Read()
	if err != nil {
		t.Fatalf("read pool: %v", err)
	}
	if len(store.Accounts) != 1 {
		t.Fatalf("pool should hold exactly one account, got %d", len(store.Accounts))
	}
	if store.Accounts[0].BearerToken != "tok-a" {
		t.Fatalf("token not persisted: %+v", store.Accounts[0])
	}
}

func TestAccountsImportRejectsBadInput(t *testing.T) {
	srv := testServer(t, true, "", "secret-key")

	if rec, _ := postImport(t, srv, `{"text":"not json"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid json should be 400, got %d", rec.Code)
	}
	if rec, _ := postImport(t, srv, `{"text":"{}"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("no accounts should be 400, got %d", rec.Code)
	}
	if rec, _ := postImport(t, srv, `{"text":"  "}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("blank text should be 400, got %d", rec.Code)
	}
}

func TestAccountsImportHonoursSiteOverride(t *testing.T) {
	srv := testServer(t, true, "", "secret-key")
	payload := `{"text":"[{\"uid\":\"u-2\",\"nickname\":\"n\",\"access_token\":\"tok-b\"}]","site":"global"}`
	rec, out := postImport(t, srv, payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	results, _ := out["results"].([]any)
	first, _ := results[0].(map[string]any)
	if first["site"] != "global" {
		t.Fatalf("site override ignored: %+v", first)
	}
}
