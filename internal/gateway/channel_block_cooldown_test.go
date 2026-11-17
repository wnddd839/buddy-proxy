package gateway

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

// TestKeepModeChannelBlockDoesNotCoolDownAccount 锁住 #1：
// keep 模式下 11128 是内容策略拒收，不是账号问题。auto 分支明确「不罚号」，
// keep 分支也绝不能把账号打进 5 分钟冷却（failureCooldown 对 11128 默认给 5m）。
// 否则排障模式反而污染号池状态。
func TestKeepModeChannelBlockDoesNotCoolDownAccount(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{
		Site:             "domestic",
		AccountsPath:     filepath.Join(dir, "accounts.json"),
		SystemPromptMode: config.SystemPromptModeKeep,
	}, nil)
	t.Cleanup(func() { _ = svc.Close() })

	created, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "a", Site: "domestic", BearerToken: "token-a", Enabled: true,
		AuthStatus: accounts.AuthStatus{UserID: "u-a"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	transport := &blockOnceTransport{}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	_, err = svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto",
		Messages: []map[string]any{
			{"role": "system", "content": unknownFingerprintMarker},
			{"role": "user", "content": "hi"},
		},
	})
	if err == nil {
		t.Fatal("keep 模式应把 11128 原样返回")
	}
	if got := len(transport.snapshotBodies()); got != 1 {
		t.Fatalf("keep 模式不得重试，实际打了 %d 次", got)
	}

	store, err := svc.Pool.Read()
	if err != nil {
		t.Fatal(err)
	}
	var got accounts.Account
	found := false
	for _, a := range store.Accounts {
		if a.ID == created.ID {
			got, found = a, true
			break
		}
	}
	if !found {
		t.Fatal("账号不见了")
	}
	if got.CooldownUntil > time.Now().UnixMilli() {
		t.Fatalf("11128 不应给账号加冷却，实际 CooldownUntil=%d（now=%d），来源=%q",
			got.CooldownUntil, time.Now().UnixMilli(), got.CooldownSource)
	}
	if got.FailedRequests != 1 {
		t.Fatalf("失败计数应为 1，实际 %d", got.FailedRequests)
	}
}

// TestAutoModeChannelBlockDoesNotCoolDownAccount 同样锁住 auto 降级路径：
// 降级分支必须在 MarkResult 之前返回，账号既不加冷却也不计失败。
func TestAutoModeChannelBlockDoesNotCoolDownAccount(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{
		Site:             "domestic",
		AccountsPath:     filepath.Join(dir, "accounts.json"),
		SystemPromptMode: config.SystemPromptModeAuto,
	}, nil)
	t.Cleanup(func() { _ = svc.Close() })

	created, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "a", Site: "domestic", BearerToken: "token-a", Enabled: true,
		AuthStatus: accounts.AuthStatus{UserID: "u-a"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	svc.Provider.HTTP = &http.Client{Transport: &blockOnceTransport{}}

	_, err = svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto",
		Messages: []map[string]any{
			{"role": "system", "content": unknownFingerprintMarker},
			{"role": "user", "content": "hi"},
		},
	})
	if err != nil {
		t.Fatalf("auto 应降级成功，实际 %v", err)
	}

	store, _ := svc.Pool.Read()
	for _, a := range store.Accounts {
		if a.ID != created.ID {
			continue
		}
		if a.CooldownUntil > time.Now().UnixMilli() {
			t.Fatalf("降级路径不应加冷却，实际 CooldownUntil=%d 来源=%q", a.CooldownUntil, a.CooldownSource)
		}
		if a.FailedRequests != 0 {
			t.Fatalf("降级路径不应计失败，实际 %d", a.FailedRequests)
		}
		return
	}
	t.Fatal("账号不见了")
}
