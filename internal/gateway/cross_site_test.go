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

// routeService 造国内+国际双站号池的 Service，并按入参装路由表。
// 注意：这里不再有任何「换区开关」——路由只做固定改写。
func routeService(t *testing.T, rules []modelroute.Rule) (*Service, map[string]accounts.Account) {
	t.Helper()
	dir := t.TempDir()
	svc := New(config.Config{
		Site:         "domestic",
		AccountsPath: filepath.Join(dir, "accounts.json"),
	}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })

	byToken := map[string]accounts.Account{}
	for _, spec := range []struct{ label, site, token string }{
		{"d1", "domestic", "token-d1"},
		{"d2", "domestic", "token-d2"},
		{"g1", "global", "token-g1"},
	} {
		acct, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
			Label: spec.label, Site: spec.site, BearerToken: spec.token, Enabled: true,
		}))
		if err != nil {
			t.Fatal(err)
		}
		byToken[spec.token] = acct
	}

	routes := modelroute.Open(filepath.Join(dir, "routes.json"))
	if err := routes.Write(modelroute.Store{Rules: rules}); err != nil {
		t.Fatal(err)
	}
	svc.ModelRoutes = routes
	return svc, byToken
}

func completeOnce(t *testing.T, svc *Service) error {
	t.Helper()
	_, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model:    "cn-test",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	return err
}

// 一条国内 → 国际的固定路由。
var fixedRoute = []modelroute.Rule{
	{FromSite: "domestic", Match: "cn-test", Site: "global", Target: "gpt-x"},
}

// 命中固定路由：国内站的请求被改到国际站执行。
func TestFixedRouteRewritesSite(t *testing.T) {
	svc, _ := routeService(t, fixedRoute)
	transport := &scriptedChatTransport{byAuth: map[string]int{
		"token-d1": http.StatusOK, "token-d2": http.StatusOK, "token-g1": http.StatusOK,
	}}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	if err := completeOnce(t, svc); err != nil {
		t.Fatalf("应成功: %v", err)
	}
	chat, _ := transport.snapshot()
	if len(chat) != 1 || chat[0] != "token-g1" {
		t.Fatalf("应使用国际号，实际 %v", chat)
	}
}

// 关键回归：路由生效但目标站没有可用号时，**直接失败**，绝不偷偷换回本站。
func TestFixedRouteNoAutoFallback(t *testing.T) {
	svc, _ := routeService(t, fixedRoute)
	transport := &scriptedChatTransport{byAuth: map[string]int{
		"token-d1": http.StatusOK, "token-d2": http.StatusOK, "token-g1": http.StatusOK,
	}}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	// 禁用全部国际号 ⇒ 路由目标是国际站，但没有号
	store, _ := svc.Pool.Read()
	for _, a := range store.Accounts {
		if a.Site == "global" {
			_, _, _ = svc.Pool.SetEnabled(a.ID, false)
		}
	}

	if err := completeOnce(t, svc); err == nil {
		t.Fatal("目标站无可用号应直接失败，不应回退到本站")
	}
	chat, _ := transport.snapshot()
	for _, token := range chat {
		if token == "token-d1" || token == "token-d2" {
			t.Fatalf("不应偷偷改用国内的号: %v", chat)
		}
	}
}

// 未配置路由的模型完全不受影响，走默认站点。
func TestFixedRouteSkipsUnconfiguredModel(t *testing.T) {
	svc, _ := routeService(t, fixedRoute)
	transport := &scriptedChatTransport{byAuth: map[string]int{
		"token-d1": http.StatusOK, "token-d2": http.StatusOK, "token-g1": http.StatusOK,
	}}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	if _, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model:    "hy3", // 不在路由表里
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	}); err != nil {
		t.Fatalf("未配置模型应正常直连: %v", err)
	}
	chat, _ := transport.snapshot()
	for _, token := range chat {
		if token == "token-g1" {
			t.Fatalf("未配置模型不该打到国际号: %v", chat)
		}
	}
}

// 固定账号不走路由、不改站点。
func TestFixedRouteSkipsFixedAccount(t *testing.T) {
	svc, byToken := routeService(t, fixedRoute)
	transport := &scriptedChatTransport{byAuth: map[string]int{
		"token-d1": http.StatusOK, "token-d2": http.StatusOK, "token-g1": http.StatusOK,
	}}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	_, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		AccountID:    byToken["token-d1"].ID,
		FixedAccount: true,
		Model:        "cn-test",
		Messages:     []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatalf("固定账号应照常处理: %v", err)
	}
	chat, _ := transport.snapshot()
	for _, token := range chat {
		if token == "token-g1" {
			t.Fatalf("固定账号不该跨区: %v", chat)
		}
	}
}

// 站内换号重试已删除：首个失败参数只允许在传输层重连，
// 不允许因为 429/5xx 就换到另一个账号。
func TestNoAccountSwitchOnNonQuotaFailure(t *testing.T) {
	svc, _ := routeService(t, nil)
	// 11102 = 模型不存在：明确不该换号，也不该重试。
	transport := &scriptedChatTransport{
		byAuth:          map[string]int{"token-d1": http.StatusBadRequest},
		errorBodyByAuth: map[string]string{"token-d1": `{"code":11102,"message":"model not found"}`},
	}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	if err := completeOnce(t, svc); err == nil {
		t.Fatal("模型不存在应直接把错误返回客户端")
	}
	chat, _ := transport.snapshot()
	if len(chat) != 1 {
		t.Fatalf("不该换号也不该重试，实际 %v", chat)
	}
}
