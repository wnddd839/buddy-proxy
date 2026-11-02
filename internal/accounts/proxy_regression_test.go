package accounts_test

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
)

func TestUpsertRejectsInvalidProxyWithoutChangingPool(t *testing.T) {
	pool := accounts.NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	defer pool.Close()
	input := sampleAccount()
	input.Proxy = "ftp://user:password@127.0.0.1:21"
	if _, _, err := pool.Upsert(input); err == nil {
		t.Fatal("invalid binding was accepted")
	}
	store, err := pool.Read()
	if err != nil || len(store.Accounts) != 0 {
		t.Fatalf("invalid import changed pool: count=%d err=%v", len(store.Accounts), err)
	}
}

func TestInvalidAssignmentAndSelectionAreRaceFree(t *testing.T) {
	pool := accounts.NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	defer pool.Close()
	account, _, err := pool.Upsert(sampleAccount())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 100 {
			if _, _, err := pool.SetProxy(account.ID, "ftp://invalid:21"); err == nil {
				t.Error("invalid proxy assignment succeeded")
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			if _, err := pool.Select(accounts.SelectOptions{}); err != nil {
				t.Error(err)
			}
		}
	}()
	wg.Wait()
}
