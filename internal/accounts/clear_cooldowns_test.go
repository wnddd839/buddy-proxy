package accounts_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
)

// 「刷新状态」的语义是人工重置：启用号的冷却标记必须被整体清空。
// 旧实现只清账号级冷却、不碰 ModelCooldowns，导致面板显示余额充足、
// 请求却仍然被 Select 以「模型冷却」直接拒绝。
func TestClearCooldownsDropsAccountAndModelMarkers(t *testing.T) {
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
	if _, _, err := pool.UpdateAccount(created.ID, func(a accounts.Account) accounts.Account {
		a.CooldownUntil = now.Add(6 * time.Hour).UnixMilli()
		a.CooldownSource = accounts.CooldownSourceUpstream
		a.ModelCooldowns = map[string]int64{
			"hy4free": now.Add(10 * time.Hour).UnixMilli(),
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
	if len(cleared.ModelCooldowns) != 0 {
		t.Fatalf("模型级冷却未清：%v", cleared.ModelCooldowns)
	}

	store, err := pool.Read()
	if err != nil {
		t.Fatal(err)
	}
	got := store.Accounts[0]
	if got.CooldownUntil != 0 || len(got.ModelCooldowns) != 0 {
		t.Fatalf("落盘后仍有冷却：%+v", got)
	}
	if !got.Enabled {
		t.Fatal("清冷却不应改动启用状态")
	}
	// 清完之后必须真的能被选中（这是整件事的目的）。
	if _, err := pool.Select(accounts.SelectOptions{Site: "domestic", Model: "hy4free"}); err != nil {
		t.Fatalf("冷却清空后仍选不中：%v", err)
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
