package gateway

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/modelroute"
)

// 同站替身（domestic→domestic）跳转后不能清空 ExcludeIDs，
// 否则会立刻重打刚刚失败的那个号。
func TestSameSiteFallbackKeepsExcludeList(t *testing.T) {
	svc := routeFixture(t, []modelroute.Rule{
		{FromSite: "domestic", Match: "hy4free", Site: "domestic", Target: "hy3",
			Mode: modelroute.ModeOnExhausted},
	})
	tr := svc.Provider.HTTP.Transport.(*scriptedChatTransport)
	tr.byAuth["token-d1"] = http.StatusTooManyRequests

	err := completeModelOnce(t, svc, "hy4free")
	if err == nil {
		t.Fatal("唯一的国内号被限流且替身也失败时应报错")
	}
	got := sentTokens(t, svc)
	// 关键：同站跳转后不能重新选中 token-d1（会出现第 3 次调用）
	if len(got) > 2 {
		t.Fatalf("同站替身重打了失败的号，调用序列=%v", got)
	}
}

// onExhausted 规则配成多跳时，第二跳必须能被消费。
// 同源+同模型的链最多两跳：同站替身一条 + 另一站一条（Validate 限制同站只能一条）。
// 这里第一跳是同站替身（国内号全停用 ⇒ 必失败），第二跳才是国际站。
// 若链没有推进，请求永远不会落到 token-g1。
func TestDeferredRouteConsumesSecondHop(t *testing.T) {
	svc := routeFixture(t, []modelroute.Rule{
		{FromSite: "domestic", Match: "hy4free", Site: "domestic", Target: "alias-1",
			Mode: modelroute.ModeOnExhausted},
		{FromSite: "domestic", Match: "hy4free", Site: "global", Target: "hy4free",
			Mode: modelroute.ModeOnExhausted},
	})
	disableSiteAccounts(t, svc, "domestic")

	if err := completeModelOnce(t, svc, "hy4free"); err != nil {
		t.Fatalf("第二跳应成功: %v", err)
	}
	got := sentTokens(t, svc)
	if len(got) != 1 || got[0] != "token-g1" {
		t.Fatalf("链未推进到第二跳，实际调用=%v", got)
	}
}

// nextSelectableAccount 必须与 Select 同口径：额度耗尽的号不能被当成可换候选。
func TestNextSelectableAccountSkipsExhausted(t *testing.T) {
	svc := routeFixture(t, nil)
	store, err := svc.Pool.Read()
	if err != nil {
		t.Fatal(err)
	}
	var domestic accounts.Account
	for _, a := range store.Accounts {
		if a.Site == "domestic" {
			domestic = a
			break
		}
	}
	now := time.Now().UnixMilli()
	zero := 0.0
	if _, err := svc.Pool.ApplyQuotaState(domestic.ID, &zero, false, now+3_600_000, now); err != nil {
		t.Fatal(err)
	}
	if id, ok := svc.nextSelectableAccount("domestic", "hy4free", nil); ok {
		t.Fatalf("额度耗尽的号不该被当成候选，却返回 %s", id)
	}
}

// hasAlternativeAccount 同理：额度耗尽的号不算备胎。
func TestHasAlternativeAccountSkipsExhausted(t *testing.T) {
	svc := routeFixture(t, nil)
	second, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "d2", Site: "domestic", BearerToken: "token-d2", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	zero := 0.0
	if _, err := svc.Pool.ApplyQuotaState(second.ID, &zero, false, now+3_600_000, now); err != nil {
		t.Fatal(err)
	}
	store, err := svc.Pool.Read()
	if err != nil {
		t.Fatal(err)
	}
	var current accounts.Account
	for _, a := range store.Accounts {
		if a.Site == "domestic" && a.ID != second.ID {
			current = a
			break
		}
	}
	if svc.hasAlternativeAccount("domestic", current.ID, nil) {
		t.Fatal("额度耗尽的号不该被算成备胎")
	}
}

var _ = context.Background
