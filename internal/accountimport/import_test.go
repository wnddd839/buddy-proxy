package accountimport

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func fakeJWT(claims map[string]any) string {
	payload, _ := json.Marshal(claims)
	enc := base64.RawURLEncoding.EncodeToString(payload)
	return "header." + enc + ".signature"
}

func TestParseCodeBuddyCredentialArray(t *testing.T) {
	token := fakeJWT(map[string]any{"sub": "uid-1", "nickname": "小明", "exp": 1795951020.0})
	raw := []map[string]any{{
		"uid":           "uid-1",
		"nickname":      "198****9015",
		"domain":        "www.codebuddy.cn",
		"expires_at":    1795951021000,
		"access_token":  token,
		"refresh_token": "refresh-1",
	}}
	text, _ := json.Marshal(raw)

	got, err := Parse(text, Options{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 account, got %d", len(got))
	}
	acc := got[0]
	if acc.Site != "domestic" {
		t.Fatalf("site should infer domestic from codebuddy.cn, got %q", acc.Site)
	}
	if acc.BearerToken != token || acc.RefreshToken != "refresh-1" {
		t.Fatalf("tokens not carried over: %+v", acc)
	}
	if acc.TokenExpiresAt != 1795951021000 {
		t.Fatalf("expires should stay in ms, got %d", acc.TokenExpiresAt)
	}
	if acc.AuthStatus.UserID != "uid-1" {
		t.Fatalf("uid=%q", acc.AuthStatus.UserID)
	}
	if acc.Label != "198****9015" {
		t.Fatalf("label should come from nickname, got %q", acc.Label)
	}
	if !acc.Enabled {
		t.Fatal("imported account should be enabled by default")
	}
	if acc.Source != "manual_import" {
		t.Fatalf("source=%q", acc.Source)
	}
	if acc.CredentialHash == "" {
		t.Fatal("credential hash should be computed for dedupe")
	}
}

func TestParseAccountAuthWrapper(t *testing.T) {
	raw := `{"account":{"enterpriseId":"","nickname":"老王","uid":"uid-2"},
	          "auth":{"accessToken":"tok-2","refreshToken":"rt-2","domain":"www.codebuddy.cn",
	                  "expiresAt":1795951021,"realm":"cn"}}`
	got, err := Parse([]byte(raw), Options{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	acc := got[0]
	if acc.Site != "domestic" {
		t.Fatalf("realm cn should map to domestic, got %q", acc.Site)
	}
	if acc.TokenExpiresAt != 1795951021000 {
		t.Fatalf("seconds should be converted to ms, got %d", acc.TokenExpiresAt)
	}
	if acc.Label != "老王" {
		t.Fatalf("label=%q", acc.Label)
	}
}

func TestParseNativeStoreRoundTrip(t *testing.T) {
	raw := `{"version":1,"provider":"codebuddy","accounts":[
	  {"id":"a1","label":"主力","enabled":false,"site":"global","bearerToken":"tok-3",
	   "refreshToken":"rt-3","authStatus":{"userName":"nick"}}]}`
	got, err := Parse([]byte(raw), Options{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].Site != "global" || got[0].BearerToken != "tok-3" {
		t.Fatalf("unexpected account: %+v", got[0])
	}
}

func TestParseDedupesSameCredential(t *testing.T) {
	raw := `[{"uid":"u","nickname":"n","domain":"www.codebuddy.cn","access_token":"same"},
	         {"uid":"u","nickname":"n","domain":"www.codebuddy.cn","access_token":"same"}]`
	got, err := Parse([]byte(raw), Options{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("duplicate credential should collapse, got %d", len(got))
	}
}

func TestParseOverrides(t *testing.T) {
	raw := `[{"uid":"u3","nickname":"z","access_token":"tok-4"}]`
	got, err := Parse([]byte(raw), Options{Site: "global", Label: "导入"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got[0].Site != "global" {
		t.Fatalf("site override ignored: %q", got[0].Site)
	}
	if !strings.HasPrefix(got[0].Label, "导入 · ") {
		t.Fatalf("label prefix ignored: %q", got[0].Label)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte("not json"), Options{}); err == nil {
		t.Fatal("expected parse error")
	}
	if _, err := Parse([]byte(`{"foo":"bar"}`), Options{}); err != ErrNoAccounts {
		t.Fatalf("expected ErrNoAccounts, got %v", err)
	}
	if _, err := Parse([]byte("   "), Options{}); err != ErrNoAccounts {
		t.Fatalf("expected ErrNoAccounts for blank, got %v", err)
	}
}

func TestInferSiteFromDomain(t *testing.T) {
	cases := map[string]string{
		"www.codebuddy.cn":    "domestic",
		"www.workbuddy.cn":    "domestic",
		"www.codebuddy.ai":    "global",
		"copilot.tencent.com": "global",
		"":                    "",
		"example.com":         "",
	}
	for domain, want := range cases {
		if got := inferSiteFromDomain(domain); got != want {
			t.Fatalf("inferSiteFromDomain(%q)=%q want %q", domain, got, want)
		}
	}
}
