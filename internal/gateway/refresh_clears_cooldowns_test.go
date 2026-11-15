package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

// 「刷新状态」重置账号级冷却，但不动模型级冷却（真实 429 的证据）。
func TestRefreshPoolUsageClearsAccountCooldownKeepsModelMarkers(t *testing.T) {
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
	modelUntil := now.Add(10 * time.Hour).UnixMilli()
	if _, _, err := svc.Pool.UpdateAccount(created.ID, func(a accounts.Account) accounts.Account {
		a.CooldownUntil = now.Add(6 * time.Hour).UnixMilli()
		a.CooldownSource = accounts.CooldownSourceUpstream
		a.ModelCooldowns = map[string]int64{"hy4free": modelUntil}
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
	if got.ModelCooldowns["hy4free"] != modelUntil {
		t.Fatalf("模型级冷却被误清：%v", got.ModelCooldowns)
	}
	if got.QuotaRemaining == nil || *got.QuotaRemaining != 42 {
		t.Fatalf("余额未落库：%v", got.QuotaRemaining)
	}

	// 管理台账号列表拿到的就是这份摘要：模型冷却必须在 JSON 里，
	// 否则前端 accountStates() 永远渲染不出「模型冷却」这条阻塞原因。
	payload, err := json.Marshal(accounts.SummarizeStore(store, ""))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"modelCooldowns":{"hy4free":`) {
		t.Fatalf("账号摘要未携带模型冷却：%s", string(payload))
	}
}
