package accounts_test

import (
	"path/filepath"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
)

// 重新导入同一账号时，已有出口绑定不能被清掉。
func TestUpsertPreservesProxyOnReimport(t *testing.T) {
	pool := accounts.NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	saved, _, err := pool.Upsert(accounts.Account{
		BearerToken: "tok-a",
		Label:       "a",
		Site:        "domestic",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := pool.SetProxy(saved.ID, "socks5h://u:p@127.0.0.1:1080"); err != nil {
		t.Fatal(err)
	}
	// 模拟「一键导入」：同一凭据、不带 proxy 字段
	again := accounts.Account{BearerToken: "tok-a", Label: "a-新", Site: "domestic"}
	if _, _, err := pool.Upsert(again); err != nil {
		t.Fatal(err)
	}
	store, _ := pool.Read()
	if len(store.Accounts) != 1 {
		t.Fatalf("应合并为 1 个账号，实际 %d", len(store.Accounts))
	}
	if store.Accounts[0].Proxy != "socks5h://u:p@127.0.0.1:1080" {
		t.Fatalf("重新导入把出口清掉了: %q", store.Accounts[0].Proxy)
	}
}
