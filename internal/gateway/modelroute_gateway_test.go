package gateway

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/modelroute"
)

// routeFixture 造一个国内 + 国际双站号池，装上给定规则，返回服务与请求构造器。
func routeFixture(t *testing.T, rules []modelroute.Rule) *Service {
	t.Helper()
	dir := t.TempDir()
	svc := New(config.Config{Site: "domestic", AccountsPath: filepath.Join(dir, "accounts.json")},
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })

	for _, spec := range []struct{ label, site, token string }{
		{"d1", "domestic", "token-d1"},
		{"g1", "global", "token-g1"},
	} {
		if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
			Label: spec.label, Site: spec.site, BearerToken: spec.token, Enabled: true,
		})); err != nil {
			t.Fatal(err)
		}
	}

	routes := modelroute.Open(filepath.Join(dir, "routes.json"))
	if err := routes.Write(modelroute.Store{Rules: rules}); err != nil {
		t.Fatal(err)
	}
	svc.ModelRoutes = routes

	svc.Provider.HTTP = &http.Client{Transport: &scriptedChatTransport{byAuth: map[string]int{
		"token-d1": http.StatusOK,
		"token-g1": http.StatusOK,
	}}}
	return svc
}

func sentTokens(t *testing.T, svc *Service) []string {
	t.Helper()
	tr, ok := svc.Provider.HTTP.Transport.(*scriptedChatTransport)
	if !ok {
		t.Fatal("transport 类型不对")
	}
	chat, _ := tr.snapshot()
	return chat
}

// 国内 → 国际：国内站收到 gpt-5.6 时改用国际站的号。
func TestRouteDomesticToGlobal(t *testing.T) {
	svc := routeFixture(t, []modelroute.Rule{
		{FromSite: "domestic", Match: "gpt-5.6", Site: "global", Target: "gpt-5.6"},
	})
	if _, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model:    "gpt-5.6",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	}); err != nil {
		t.Fatalf("路由后应成功: %v", err)
	}
	if got := sentTokens(t, svc); len(got) != 1 || got[0] != "token-g1" {
		t.Fatalf("应使用国际号，实际 %v", got)
	}
}

// 国际 → 国内：反方向必须能独立配置，且只在国际站收到请求时生效。
func TestRouteGlobalToDomestic(t *testing.T) {
	svc := routeFixture(t, []modelroute.Rule{
		{FromSite: "global", Match: "glm-5.3", Site: "domestic", Target: "glm-5.3"},
	})
	if _, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Site:     "global",
		Model:    "glm-5.3",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	}); err != nil {
		t.Fatalf("路由后应成功: %v", err)
	}
	if got := sentTokens(t, svc); len(got) != 1 || got[0] != "token-d1" {
		t.Fatalf("应使用国内号，实际 %v", got)
	}
}

// 方向不匹配：国内 → 国际的规则，不能在国际站收到同名请求时生效。
func TestRouteDirectionMismatchDoesNotApply(t *testing.T) {
	svc := routeFixture(t, []modelroute.Rule{
		{FromSite: "domestic", Match: "gpt-5.6", Site: "global", Target: "gpt-5.6"},
	})
	// 请求本身落在国际站（未显式指定，靠 Site 字段）
	if _, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Site:     "global",
		Model:    "gpt-5.6",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	}); err != nil {
		t.Fatalf("应直连国际站: %v", err)
	}
	if got := sentTokens(t, svc); len(got) != 1 || got[0] != "token-g1" {
		t.Fatalf("方向不匹配时不该改写，实际 %v", got)
	}
}

// 客户端显式指定站点（X-Site 头或 cn:/global: 前缀）时，路由完全不生效。
func TestRouteSkippedWhenSiteExplicit(t *testing.T) {
	svc := routeFixture(t, []modelroute.Rule{
		{FromSite: "domestic", Match: "gpt-5.6", Site: "global", Target: "gpt-5.6"},
		{FromSite: "global", Match: "gpt-5.6", Site: "domestic", Target: "glm-5.3"},
	})
	// 客户端明确要国内站：即使「国内→国际」的规则在，也必须留在国内
	if _, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Site:         "domestic",
		SiteExplicit: true,
		Model:        "gpt-5.6",
		Messages:     []map[string]any{{"role": "user", "content": "hi"}},
	}); err != nil {
		t.Fatalf("显式站点请求应照常处理: %v", err)
	}
	if got := sentTokens(t, svc); len(got) != 1 || got[0] != "token-d1" {
		t.Fatalf("显式指定国内站不该被改到国际，实际 %v", got)
	}
}

// 带 cn:/global: 前缀的模型名不命中路由（前缀由上层剥离，这里也兜底）。
func TestRouteSkipsPrefixedModel(t *testing.T) {
	svc := routeFixture(t, []modelroute.Rule{
		{FromSite: "domestic", Match: "gpt-5.6", Site: "global", Target: "gpt-5.6"},
	})
	if _, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model:    "cn:gpt-5.6",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	}); err != nil {
		t.Fatalf("显式前缀请求应照常处理: %v", err)
	}
	if got := sentTokens(t, svc); len(got) != 1 || got[0] != "token-d1" {
		t.Fatalf("带 cn: 前缀应用国内号，实际 %v", got)
	}
}

// 固定账号请求不走路由（账号已被 Key 硬绑定，改站点必然 site mismatch）。
func TestRouteSkipsFixedAccount(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{Site: "domestic", AccountsPath: filepath.Join(dir, "accounts.json")},
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })

	a, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "d1", Site: "domestic", BearerToken: "token-d1", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}

	routes := modelroute.Open(filepath.Join(dir, "routes.json"))
	if err := routes.Write(modelroute.Store{Rules: []modelroute.Rule{
		{FromSite: "domestic", Match: "gpt-5.6", Site: "global", Target: "gpt-5.6"},
	}}); err != nil {
		t.Fatal(err)
	}
	svc.ModelRoutes = routes
	svc.Provider.HTTP = &http.Client{Transport: &scriptedChatTransport{
		byAuth: map[string]int{"token-d1": http.StatusOK},
	}}

	if _, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		AccountID:    a.ID,
		FixedAccount: true,
		Model:        "gpt-5.6",
		Messages:     []map[string]any{{"role": "user", "content": "hi"}},
	}); err != nil {
		t.Fatalf("固定账号请求应照常处理: %v", err)
	}
	if got := sentTokens(t, svc); len(got) != 1 || got[0] != "token-d1" {
		t.Fatalf("固定账号应用绑定号，实际 %v", got)
	}
}
