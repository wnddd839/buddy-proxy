package accounts_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
)

// 「刷新状态」重置的是**账号级**冷却；模型级 ModelCooldowns 是上游真实 429
// 的证据，不能被一次余额刷新抹掉。
func TestClearCooldownsResetsAccountCooldownOnly(t *testing.T) {
	pool := accounts.NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	t.Cleanup(func() { _ = pool.Close() })

	created, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "reset", Site: "domestic", BearerToken: "eyJhbGciOiJIUzI1NiJ9.e30.reset",
		Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	modelUntil := now.Add(10 * time.Hour).UnixMilli()
	if _, _, err := pool.UpdateAccount(created.ID, func(a accounts.Account) accounts.Account {
		a.CooldownUntil = now.Add(6 * time.Hour).UnixMilli()
		a.CooldownSource = accounts.CooldownSourceUpstream
		a.ModelCooldowns = map[string]int64{
			"hy4free": modelUntil,
			"hy3":     now.Add(2 * time.Hour).UnixMilli(),
		}
		return a
	}); err != nil {
		t.Fatal(err)
	}

	cleared, err := pool.ClearCooldowns(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.CooldownUntil != 0 || cleared.CooldownSource != "" {
		t.Fatalf("账号级冷却未清：until=%d source=%q", cleared.CooldownUntil, cleared.CooldownSource)
	}
	if len(cleared.ModelCooldowns) != 2 {
		t.Fatalf("模型级冷却被误清：%v", cleared.ModelCooldowns)
	}
	if cleared.ModelCooldowns["hy4free"] != modelUntil {
		t.Fatalf("hy4free 冷却被动过：%d", cleared.ModelCooldowns["hy4free"])
	}

	store, err := pool.Read()
	if err != nil {
		t.Fatal(err)
	}
	got := store.Accounts[0]
	if got.CooldownUntil != 0 {
		t.Fatalf("落盘后账号级冷却仍在：%d", got.CooldownUntil)
	}
	if len(got.ModelCooldowns) != 2 {
		t.Fatalf("落盘后模型冷却被误清：%v", got.ModelCooldowns)
	}
	if !got.Enabled {
		t.Fatal("清冷却不应改动启用状态")
	}

	// 账号级冷却清掉后，不带模型选号必须能选中。
	if _, err := pool.Select(accounts.SelectOptions{Site: "domestic"}); err != nil {
		t.Fatalf("账号级冷却清空后仍选不中：%v", err)
	}
	// 模型仍在限流窗口内：按模型选号必须继续拒绝（这是上游的真实证据）。
	if _, err := pool.Select(accounts.SelectOptions{Site: "domestic", Model: "hy4free"}); err == nil {
		t.Fatal("模型冷却仍在，按 hy4free 选号不应成功")
	}
}

// 停用的号不在轮换里，保持原状：下次启用时仍能看到它曾被上游限流的证据。
func TestClearCooldownsLeavesDisabledAccountsAlone(t *testing.T) {
	pool := accounts.NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	t.Cleanup(func() { _ = pool.Close() })

	created, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "off", Site: "domestic", BearerToken: "eyJhbGciOiJIUzI1NiJ9.e30.off",
	}))
	if err != nil {
		t.Fatal(err)
	}
	// CreateAccount 默认启用（enabledSet 是包内字段，外部测试构造不出 false），
	// 所以走 SetEnabled 真正停用。
	if _, _, err := pool.SetEnabled(created.ID, false); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	until := now.Add(3 * time.Hour).UnixMilli()
	if _, _, err := pool.UpdateAccount(created.ID, func(a accounts.Account) accounts.Account {
		a.CooldownUntil = until
		a.ModelCooldowns = map[string]int64{"hy4free": until}
		return a
	}); err != nil {
		t.Fatal(err)
	}

	got, err := pool.ClearCooldowns(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CooldownUntil != until {
		t.Fatalf("停用号的冷却被误清：%d", got.CooldownUntil)
	}
	if got.ModelCooldowns["hy4free"] != until {
		t.Fatalf("停用号的模型冷却被误清：%v", got.ModelCooldowns)
	}
}
