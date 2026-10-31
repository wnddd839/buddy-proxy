package accounts_test

import (
	"path/filepath"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
)

func sampleAccount() accounts.Account {
	return accounts.Account{
		BearerToken: "eyJhbGciOiJIUzI1NiJ9.e30.sig",
		Label:       "warp-test",
		Site:        "global",
	}
}

func TestNormalizeAccountProxy(t *testing.T) {
	good := accounts.NormalizeAccount(accounts.Account{
		BearerToken: "tok",
		Label:       "a",
		Proxy:       "  socks5h://u:p@127.0.0.1:1080  ",
	}, 1)
	if good.Proxy != "socks5h://u:p@127.0.0.1:1080" {
		t.Fatalf("合法出口应保留（去空格）: %q", good.Proxy)
	}
	bad := accounts.NormalizeAccount(accounts.Account{
		BearerToken: "tok",
		Label:       "b",
		Proxy:       "ftp://u:p@127.0.0.1:1080",
	}, 1)
	if bad.Proxy != "" {
		t.Fatalf("非法 scheme 应被丢弃，实际 %q", bad.Proxy)
	}
}

func TestSummaryMasksProxyPassword(t *testing.T) {
	account := accounts.NormalizeAccount(accounts.Account{
		BearerToken: "tok",
		Label:       "a",
		Proxy:       "socks5h://alice:s3cret@127.0.0.1:1080",
	}, 1)
	summary := accounts.SummarizeAccount(account)
	if summary.Proxy != "socks5h://alice:***@127.0.0.1:1080" {
		t.Fatalf("摘要里的出口应打码: %q", summary.Proxy)
	}
}

func TestPoolSetAndClearProxy(t *testing.T) {
	pool := accounts.NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	saved, _, err := pool.Upsert(sampleAccount())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := pool.SetProxy(saved.ID, "socks5h://u:p@127.0.0.1:1080"); err != nil {
		t.Fatal(err)
	}
	store, err := pool.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Accounts) != 1 || store.Accounts[0].Proxy != "socks5h://u:p@127.0.0.1:1080" {
		t.Fatalf("出口未落盘: %+v", store.Accounts)
	}
	// 非法出口要被拒绝，而不是静默写空。
	if _, _, err := pool.SetProxy(saved.ID, "ftp://x:1"); err == nil {
		t.Fatal("非法出口应返回错误")
	}
	if _, _, err := pool.SetProxy(saved.ID, ""); err != nil {
		t.Fatal(err)
	}
	store, _ = pool.Read()
	if store.Accounts[0].Proxy != "" {
		t.Fatalf("清空出口失败: %q", store.Accounts[0].Proxy)
	}
}

func TestPoolAssignProxiesRoundRobin(t *testing.T) {
	pool := accounts.NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	var ids []string
	for _, token := range []string{"tok-a", "tok-b", "tok-c"} {
		acc := sampleAccount()
		acc.BearerToken = token
		saved, _, err := pool.Upsert(acc)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, saved.ID)
	}
	assignments := map[string]string{
		ids[0]: "socks5h://u:p@127.0.0.1:1080",
		ids[1]: "socks5h://u:p@127.0.0.1:1081",
		ids[2]: "socks5h://u:p@127.0.0.1:1080",
	}
	if _, _, err := pool.AssignProxies(assignments); err != nil {
		t.Fatal(err)
	}
	store, err := pool.Read()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, acc := range store.Accounts {
		got[acc.ID] = acc.Proxy
	}
	for id, want := range assignments {
		if got[id] != want {
			t.Fatalf("账号 %s 出口=%q want=%q", id, got[id], want)
		}
	}
	if _, _, err := pool.AssignProxies(map[string]string{"nope": "socks5h://u:p@127.0.0.1:1080"}); err == nil {
		t.Fatal("不存在的账号应报错")
	}
}
