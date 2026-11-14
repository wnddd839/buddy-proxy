package gateway

import (
	"context"
	"net/http"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/modelroute"
)

func completeModelOnce(t *testing.T, svc *Service, model string) error {
	t.Helper()
	_, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model:    model,
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	return err
}

func disableSiteAccounts(t *testing.T, svc *Service, site string) {
	t.Helper()
	store, err := svc.Pool.Read()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range store.Accounts {
		if a.Site != site {
			continue
		}
		if _, _, err := svc.Pool.SetEnabled(a.ID, false); err != nil {
			t.Fatal(err)
		}
	}
}

// onExhausted：源站点还有号时，一行都不能改写。
func TestRouteOnExhaustedKeepsSourceWhileUsable(t *testing.T) {
	svc := routeFixture(t, []modelroute.Rule{
		{FromSite: "domestic", Match: "hy4free", Site: "global", Target: "hy4free", Mode: modelroute.ModeOnExhausted},
	})
	if err := completeModelOnce(t, svc, "hy4free"); err != nil {
		t.Fatalf("源站有号时应正常回源: %v", err)
	}
	if got := sentTokens(t, svc); len(got) != 1 || got[0] != "token-d1" {
		t.Fatalf("应使用国内号，实际 %v", got)
	}
}

// always（含没写 mode 的老规则）仍然立即改写。
func TestRouteAlwaysStillRewritesImmediately(t *testing.T) {
	svc := routeFixture(t, []modelroute.Rule{
		{FromSite: "domestic", Match: "hy4free", Site: "global", Target: "hy4free"},
	})
	if err := completeModelOnce(t, svc, "hy4free"); err != nil {
		t.Fatalf("应成功: %v", err)
	}
	if got := sentTokens(t, svc); len(got) != 1 || got[0] != "token-g1" {
		t.Fatalf("always 应立即改写，实际 %v", got)
	}
}

// onExhausted：源站点选不到号（全禁用）时才跳。
func TestRouteOnExhaustedJumpsWhenSourceHasNoAccounts(t *testing.T) {
	svc := routeFixture(t, []modelroute.Rule{
		{FromSite: "domestic", Match: "hy4free", Site: "global", Target: "hy4free", Mode: modelroute.ModeOnExhausted},
	})
	disableSiteAccounts(t, svc, "domestic")
	if err := completeModelOnce(t, svc, "hy4free"); err != nil {
		t.Fatalf("源站无号时应跳到国际站: %v", err)
	}
	if got := sentTokens(t, svc); len(got) != 1 || got[0] != "token-g1" {
		t.Fatalf("应使用国际号，实际 %v", got)
	}
}

// 请求中途 429，且同区已无号可换 ⇒ 这时候才算「用尽」，可以跨区。
func TestRouteOnExhaustedJumpsAfterQuotaFailureWithNoAlternative(t *testing.T) {
	svc := routeFixture(t, []modelroute.Rule{
		{FromSite: "domestic", Match: "hy4free", Site: "global", Target: "hy4free", Mode: modelroute.ModeOnExhausted},
	})
	tr := svc.Provider.HTTP.Transport.(*scriptedChatTransport)
	tr.byAuth["token-d1"] = http.StatusTooManyRequests
	if err := completeModelOnce(t, svc, "hy4free"); err != nil {
		t.Fatalf("同区无号可换时应跨区重试: %v", err)
	}
	if got := sentTokens(t, svc); len(got) != 2 || got[0] != "token-d1" || got[1] != "token-g1" {
		t.Fatalf("应先打国内再跳国际，实际 %v", got)
	}
}

// 同区还有号可换时只换号，绝不跨区 —— 否则跨区规则会偷偷吃掉另一区的额度。
func TestRouteOnExhaustedDoesNotJumpWhenSameSiteAlternativeExists(t *testing.T) {
	svc := routeFixture(t, []modelroute.Rule{
		{FromSite: "domestic", Match: "hy4free", Site: "global", Target: "hy4free", Mode: modelroute.ModeOnExhausted},
	})
	if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "d2", Site: "domestic", BearerToken: "token-d2", Enabled: true,
	})); err != nil {
		t.Fatal(err)
	}
	tr := svc.Provider.HTTP.Transport.(*scriptedChatTransport)
	tr.byAuth["token-d1"] = http.StatusTooManyRequests
	tr.byAuth["token-d2"] = http.StatusOK
	if err := completeModelOnce(t, svc, "hy4free"); err != nil {
		t.Fatalf("应换到同区第二个号: %v", err)
	}
	got := sentTokens(t, svc)
	if len(got) != 2 || got[1] != "token-d2" {
		t.Fatalf("应在同区换号，实际 %v", got)
	}
	for _, token := range got {
		if token == "token-g1" {
			t.Fatalf("同区还有号时不该跨区，实际 %v", got)
		}
	}
}

// 目标站点也没有可用号 ⇒ 留在原地报错，不做注定 429 的跨区。
func TestRouteOnExhaustedSkipsJumpWhenTargetUnusable(t *testing.T) {
	svc := routeFixture(t, []modelroute.Rule{
		{FromSite: "domestic", Match: "hy4free", Site: "global", Target: "hy4free", Mode: modelroute.ModeOnExhausted},
	})
	disableSiteAccounts(t, svc, "domestic")
	disableSiteAccounts(t, svc, "global")
	if err := completeModelOnce(t, svc, "hy4free"); err == nil {
		t.Fatal("两个区都没号时应报错")
	}
	if got := sentTokens(t, svc); len(got) != 0 {
		t.Fatalf("不该打上游，实际 %v", got)
	}
}

// 客户端显式钉了站点（cn:/global:）时，onExhausted 也不能把它搬到另一个区。
func TestRouteOnExhaustedRespectsExplicitSite(t *testing.T) {
	svc := routeFixture(t, []modelroute.Rule{
		{FromSite: "domestic", Match: "hy4free", Site: "global", Target: "hy4free", Mode: modelroute.ModeOnExhausted},
	})
	disableSiteAccounts(t, svc, "domestic")
	_, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Site:         "domestic",
		SiteExplicit: true,
		Model:        "hy4free",
		Messages:     []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err == nil {
		t.Fatal("显式钉国内且国内无号时应报错，不能改到国际站")
	}
	if got := sentTokens(t, svc); len(got) != 0 {
		t.Fatalf("不该打上游，实际 %v", got)
	}
}
