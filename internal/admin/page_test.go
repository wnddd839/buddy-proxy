package admin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedPageHTMLIntegrity(t *testing.T) {
	page := PageHTML()
	if len(page) < 1024 {
		t.Fatalf("embedded admin page looks unexpectedly small: %d bytes", len(page))
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(page)), "<!doctype html>") {
		t.Fatal("embedded admin page must start with <!doctype html>")
	}
	wantMeta := `content="` + UIRevision + `"`
	if !strings.Contains(page, wantMeta) {
		t.Fatalf("embedded page missing UIRevision meta %s", wantMeta)
	}
	if !strings.Contains(page, `id="root"`) {
		t.Fatal("embedded page missing React mount root")
	}
	if !strings.Contains(page, ".pill{") || !strings.Contains(page, "white-space:nowrap") {
		t.Fatal("embedded CSS must keep .pill white-space:nowrap invariant")
	}
}

func TestZeroExternalCDN(t *testing.T) {
	page := PageHTML()
	for _, banned := range []string{
		"fonts.googleapis.com",
		"fonts.gstatic.com",
		"cdn.jsdelivr.net",
		"unpkg.com",
		"cdnjs.cloudflare.com",
	} {
		if strings.Contains(page, banned) {
			t.Fatalf("embedded admin page must be 100%% offline and not reference %q", banned)
		}
	}
}

func TestManualUpdateContract(t *testing.T) {
	page := PageHTML()
	for _, want := range []string{
		"检查更新",
		"更新并重启",
		"等待服务重新连接",
		"AbortController",
		"/direct-admin/api/update",
		"/direct-admin/api/update/check",
		"/direct-admin/api/update/install",
		"/direct-admin/api/update/cancel",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("embedded bundle missing manual update contract %q", want)
		}
	}
	// If web source exists alongside the Go tree, verify no automatic release polling exists.
	updateSrc := filepath.Join("..", "..", "web", "src", "components", "layout", "UpdateWidget.tsx")
	if b, err := os.ReadFile(updateSrc); err == nil {
		s := string(b)
		if strings.Contains(s, "setInterval(") {
			t.Fatal("UpdateWidget must never poll GitHub release checks on an interval")
		}
	}
}

func TestAdminCoreEndpointsWired(t *testing.T) {
	page := PageHTML()
	for _, endpoint := range []string{
		"/direct-admin/api/status",
		"?fresh=1",
		"/direct-admin/api/concurrency",
		"/direct-admin/api/usage",
		"/direct-admin/api/pool-site",
		"/direct-admin/api/pool-product",
		"/direct-admin/api/client-config",
		"/direct-admin/api/client-config/bound-keys",
		"/direct-admin/api/checkin-config",
		"/direct-admin/api/checkin-log",
		"/direct-admin/api/model-routes",
		"/direct-admin/api/quota-floor",
		"/direct-admin/api/system-prompt",
		"/direct-admin/api/system-prompt/events",
		"/direct-admin/api/system-dryrun",
		"/direct-admin/api/invariants",
		"/direct-admin/api/tok-baselines",
		"/direct-admin/api/tok-fingerprint",
		"/direct-admin/api/tok-identity-check",
		"/direct-admin/api/codebuddy/proxies",
		"/direct-admin/api/codebuddy/accounts/import",
	} {
		if !strings.Contains(page, endpoint) {
			t.Errorf("embedded bundle missing admin API endpoint %q", endpoint)
		}
	}
}

func TestAdminSourceInvariants(t *testing.T) {
	appSrc := filepath.Join("..", "..", "web", "src", "App.tsx")
	b, err := os.ReadFile(appSrc)
	if err != nil {
		t.Skip("web/src not present in build context")
	}
	s := string(b)
	if !strings.Contains(s, "refreshStatus(true)") {
		t.Fatal("refresh button must call refreshStatus(true)")
	}
	if !strings.Contains(s, "refreshStatus(false)") {
		t.Fatal("background 15s poll must call refreshStatus(false)")
	}
	if strings.Contains(s, "setInterval(() => {\n      if (document.visibilityState === 'hidden') return;\n      refreshStatus(true)") {
		t.Fatal("15s poll must never pass fresh=true")
	}
}

func TestLaunchPageEscapesXSS(t *testing.T) {
	html := LaunchPage(`<img src=x onerror="alert(1)">`, false)
	if strings.Contains(html, `<img src=x`) {
		t.Fatal("LaunchPage failed to escape HTML message")
	}
	if !strings.Contains(html, `&lt;img src=x`) {
		t.Fatal("LaunchPage missing escaped message text")
	}
	if !strings.Contains(html, `href="/direct-admin/#codebuddy"`) {
		t.Fatal("LaunchPage must link back to /direct-admin/#codebuddy")
	}
}
