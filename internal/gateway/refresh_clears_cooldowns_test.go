package gateway

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

// 「刷新状态」= 人工重置。启用号刷新成功后，账号级与模型级冷却都必须清空，
// 否则会出现「面板显示余额充足、请求却报 rate limited」的矛盾。
func TestRefreshPoolUsageClearsCooldownMarkers(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{Site: "domestic", AccountsPath: filepath.Join(dir, "accounts.json")},
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	transport := &scriptedChatTransport{creditsByAuth: map[string]float64{"token-reset": 42}}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	created, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "reset", Site: "domestic", BearerToken: "token-reset", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, _, err := svc.Pool.UpdateAccount(created.ID, func(a accounts.Account) accounts.Account {
		a.CooldownUntil = now.Add(6 * time.Hour).UnixMilli()
		a.CooldownSource = accounts.CooldownSourceUpstream
		a.ModelCooldowns = map[string]int64{"hy4free": now.Add(10 * time.Hour).UnixMilli()}
		return a
	}); err != nil {
		t.Fatal(err)
	}

	items := svc.RefreshPoolUsage(context.Background(), "")
	if len(items) != 1 || !items[0].OK {
		t.Fatalf("刷新未成功：%+v", items)
	}

	store, err := svc.Pool.Read()
	if err != nil {
		t.Fatal(err)
	}
	got := store.Accounts[0]
	if got.CooldownUntil != 0 || got.CooldownSource != "" {
		t.Fatalf("刷新后账号级冷却未清：until=%d source=%q", got.CooldownUntil, got.CooldownSource)
	}
	if len(got.ModelCooldowns) != 0 {
		t.Fatalf("刷新后模型级冷却未清：%v", got.ModelCooldowns)
	}
	if got.QuotaRemaining == nil || *got.QuotaRemaining != 42 {
		t.Fatalf("余额未落库：%v", got.QuotaRemaining)
	}
}
