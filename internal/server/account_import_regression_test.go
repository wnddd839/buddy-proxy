package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/accountimport"
	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/oauth"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

func importJSON(t *testing.T, srv *Server, text string) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"text": text})
	_, result := postImport(t, srv, string(body))
	return result
}

func TestImportPreservesOmittedAccountStateAndRejectsRefreshOnly(t *testing.T) {
	srv := testServer(t, true, "", "secret-key")
	saved, _, err := srv.Svc.Pool.Upsert(accounts.Account{
		Site: "domestic", BearerToken: "old-access", RefreshToken: "old-refresh",
		Label: "custom label", EnterpriseID: "enterprise", TenantID: "tenant",
		DepartmentFullName: "department", Proxy: "socks5h://127.0.0.1:1080",
		AuthStatus: accounts.AuthStatus{UserID: "u"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.Svc.Pool.SetEnabled(saved.ID, false); err != nil {
		t.Fatal(err)
	}
	result := importJSON(t, srv, `{"uid":"u","site":"domestic","access_token":"new-access"}`)
	if result["updated"] != float64(1) {
		t.Fatalf("expected one update: %+v", result)
	}
	store, _ := srv.Svc.Pool.Read()
	got := store.Accounts[0]
	if got.Enabled || got.BearerToken != "new-access" || got.RefreshToken != "old-refresh" ||
		got.Label != "custom label" || got.EnterpriseID != "enterprise" ||
		got.TenantID != "tenant" || got.DepartmentFullName != "department" ||
		got.Proxy != "socks5h://127.0.0.1:1080" {
		t.Fatal("import overwrote omitted state")
	}
	payload, _ := json.Marshal(map[string]string{"text": `{"uid":"u","site":"domestic","refresh_token":"new-refresh"}`})
	rec, _ := postImport(t, srv, string(payload))
	store, _ = srv.Svc.Pool.Read()
	if rec.Code != http.StatusBadRequest || len(store.Accounts) != 1 || store.Accounts[0].BearerToken != "new-access" {
		t.Fatal("refresh-only import mutated or erased existing account")
	}
}

func TestImportMixedFailuresKeepOriginalIndexes(t *testing.T) {
	srv := testServer(t, true, "", "secret-key")
	result := importJSON(t, srv, `[{"uid":"good","access_token":"ok"},{"uid":"bad","access_token":123},42]`)
	if result["added"] != float64(1) || result["failed"] != float64(2) || result["ok"] != false {
		t.Fatalf("incorrect mixed result: %+v", result)
	}
	rows := result["results"].([]any)
	for index, row := range rows {
		if row.(map[string]any)["index"] != float64(index) {
			t.Fatalf("lost source index: %+v", row)
		}
	}
}

func TestImportRotatedCredentialsCountAsUpdateWithinBatch(t *testing.T) {
	srv := testServer(t, true, "", "secret-key")
	result := importJSON(t, srv, `[{"uid":"u","site":"domestic","access_token":"old"},{"uid":"u","site":"domestic","access_token":"new"}]`)
	if result["added"] != float64(1) || result["updated"] != float64(1) {
		t.Fatalf("incorrect batch counters: %+v", result)
	}
}

func TestImportMatchesExistingOAuthJWTIdentity(t *testing.T) {
	srv := testServer(t, true, "", "secret-key")
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"synthetic@example.invalid","preferred_username":"login","sub":"subject"}`))
	_, _, err := srv.Svc.Pool.Upsert(oauth.AccountFromTokenData(
		&oauth.TokenData{BearerToken: "header." + claims + ".old"}, "domestic", "OAuth label"))
	if err != nil {
		t.Fatal(err)
	}
	text, _ := json.Marshal(map[string]string{
		"uid": "subject", "site": "domestic", "access_token": "header." + claims + ".new",
	})
	result := importJSON(t, srv, string(text))
	store, _ := srv.Svc.Pool.Read()
	if result["updated"] != float64(1) || len(store.Accounts) != 1 {
		t.Fatalf("rotated OAuth credential duplicated account: added=%v updated=%v count=%d",
			result["added"], result["updated"], len(store.Accounts))
	}
}

func TestImportRetainsEnterpriseWrapperAndAlignsForcedSite(t *testing.T) {
	parsed, err := accountimport.Parse([]byte(`{"account":{"uid":"u","enterpriseId":"e","tenantId":"t","departmentFullName":"d"},"auth":{"accessToken":"a"}}`), accountimport.Options{})
	if err != nil || parsed[0].EnterpriseID != "e" || parsed[0].TenantID != "t" || parsed[0].DepartmentFullName != "d" {
		t.Fatal("enterprise wrapper lost functional metadata")
	}
	parsed, err = accountimport.Parse([]byte(`{"bearerToken":"a","site":"domestic","baseUrl":"https://www.codebuddy.cn","internetEnvironment":"domestic","authStatus":{"userId":"u"}}`), accountimport.Options{Site: "global"})
	if err != nil {
		t.Fatal(err)
	}
	a := parsed[0]
	if provider.RegionOf(provider.ChatOptions{Site: a.Site, InternetEnvironment: a.InternetEnvironment, BaseURL: a.BaseURL}) != "global" {
		t.Fatal("forced global account still routes domestically")
	}
}
