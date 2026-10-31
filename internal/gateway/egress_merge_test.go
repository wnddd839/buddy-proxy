package gateway

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/oauth"
)

// 刷新令牌只该更新凭证，运维字段（尤其出口）必须原样保留。
func TestMergeRefreshedTokenPreservesOpsFields(t *testing.T) {
	quota := 12.5
	existing := accounts.Account{
		ID:                  "a1",
		Label:               "号A",
		Enabled:             false,
		Source:              "import",
		Site:                "global",
		BaseURL:             "https://www.codebuddy.ai",
		InternetEnvironment: "public",
		Transport:           "protocol_direct",
		Proxy:               "socks5h://u:p@127.0.0.1:1080",
		EnterpriseID:        "ent-1",
		TenantID:            "ten-1",
		DepartmentFullName:  "dept",
		CreatedAt:           5,
		SuccessRequests:     7,
		FailedRequests:      2,
		LastUsedAt:          11,
		LastSelectedAt:      12,
		CooldownUntil:       99,
		QuotaRemaining:      &quota,
		QuotaUnlimited:      true,
		QuotaResetAt:        77,
	}
	token := &oauth.TokenData{
		BearerToken:  "new-bearer",
		RefreshToken: "new-refresh",
		ExpiresIn:    3600,
	}

	got := MergeRefreshedToken(existing, token)

	// 凭证：应该换成新的
	if got.BearerToken != "new-bearer" {
		t.Fatalf("bearer 未更新: %q", got.BearerToken)
	}
	if got.RefreshToken != "new-refresh" {
		t.Fatalf("refresh token 未更新: %q", got.RefreshToken)
	}
	if got.TokenExpiresAt == 0 {
		t.Fatal("过期时间未设置")
	}

	// 运维字段：一个都不能丢
	if got.Proxy != existing.Proxy {
		t.Fatalf("出口被清掉: %q", got.Proxy)
	}
	if got.Enabled {
		t.Fatal("启用状态被改写")
	}
	if got.Source != "import" {
		t.Fatalf("来源被改写: %q", got.Source)
	}
	if got.Site != "global" || got.BaseURL != existing.BaseURL {
		t.Fatalf("站点/基址被改写: %q %q", got.Site, got.BaseURL)
	}
	if got.EnterpriseID != "ent-1" || got.TenantID != "ten-1" || got.DepartmentFullName != "dept" {
		t.Fatal("租户信息丢失")
	}
	if got.CreatedAt != 5 || got.SuccessRequests != 7 || got.FailedRequests != 2 {
		t.Fatal("计数/创建时间丢失")
	}
	if got.LastUsedAt != 11 || got.LastSelectedAt != 12 || got.CooldownUntil != 99 {
		t.Fatal("调度字段丢失")
	}
	if got.QuotaRemaining == nil || *got.QuotaRemaining != 12.5 || !got.QuotaUnlimited || got.QuotaResetAt != 77 {
		t.Fatal("配额字段丢失")
	}
}
