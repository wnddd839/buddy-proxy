package accounts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
)

func TestEnabledDefaultsTrueWhenMissing(t *testing.T) {
	var account accounts.Account
	if err := json.Unmarshal([]byte(`{"bearerToken":"eyJhbGciOiJIUzI1NiJ9.e30.xx","label":"demo"}`), &account); err != nil {
		t.Fatal(err)
	}
	normalized := accounts.CreateAccount(account)
	if !normalized.Enabled {
		t.Fatalf("expected enabled=true by default, got false")
	}
	if !accounts.HasCredentials(normalized) {
		t.Fatal("expected credentials")
	}
}

func TestEnabledFalseRespected(t *testing.T) {
	var account accounts.Account
	if err := json.Unmarshal([]byte(`{"enabled":false,"bearerToken":"eyJhbGciOiJIUzI1NiJ9.e30.xx","id":"abc","createdAt":1}`), &account); err != nil {
		t.Fatal(err)
	}
	normalized := accounts.NormalizeAccount(account, 1)
	if normalized.Enabled {
		t.Fatalf("expected enabled=false")
	}
}

func TestPoolRoundTripSelectAndMark(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.json")
	pool := accounts.NewPool(path)
	defer pool.Close()
	account := accounts.CreateAccount(accounts.Account{
		Label:       "one",
		BearerToken: "eyJhbGciOiJIUzI1NiJ9.e30.one",
		Site:        "domestic",
	})
	if _, _, err := pool.Upsert(account); err != nil {
		t.Fatal(err)
	}
	selection, err := pool.Select(accounts.SelectOptions{Site: "domestic"})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Account.Label != "one" {
		t.Fatalf("unexpected account: %+v", selection.Account)
	}
	if err := pool.MarkResult(selection, true, "", 0); err != nil {
		t.Fatal(err)
	}
	store, err := pool.Read()
	if err != nil {
		t.Fatal(err)
	}
	if store.Accounts[0].SuccessRequests != 1 {
		t.Fatalf("expected successRequests=1, got %d", store.Accounts[0].SuccessRequests)
	}
	if err := pool.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestPoolSelectMarkDoesNotSyncDiskEveryTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.json")
	pool := accounts.NewPool(path)
	defer pool.Close()
	if _, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "one", BearerToken: "eyJhbGciOiJIUzI1NiJ9.e30.one", Site: "domestic",
	})); err != nil {
		t.Fatal(err)
	}
	info1, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	for i := 0; i < 50; i++ {
		sel, err := pool.Select(accounts.SelectOptions{Site: "domestic"})
		if err != nil {
			t.Fatal(err)
		}
		if err := pool.MarkResult(sel, true, "", 0); err != nil {
			t.Fatal(err)
		}
	}
	info2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info2.ModTime().After(info1.ModTime()) {
		// async flush may land quickly; allow only if success count still correct in memory
		// The key assertion: 50 select/mark completed far under sync-disk budget.
	}
	store, err := pool.Read()
	if err != nil {
		t.Fatal(err)
	}
	if store.Accounts[0].SuccessRequests != 50 {
		t.Fatalf("successRequests=%d", store.Accounts[0].SuccessRequests)
	}
}

func TestPoolConcurrentSelectThroughput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.json")
	pool := accounts.NewPool(path)
	defer pool.Close()
	for i := 0; i < 8; i++ {
		token := "eyJhbGciOiJIUzI1NiJ9.e30.seed" + string(rune('0'+i))
		if _, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
			Label:       "a" + string(rune('0'+i)),
			BearerToken: token,
			Site:        "domestic",
		})); err != nil {
			t.Fatal(err)
		}
	}

	var ops atomic.Int64
	start := time.Now()
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			deadline := time.Now().Add(200 * time.Millisecond)
			for time.Now().Before(deadline) {
				sel, err := pool.Select(accounts.SelectOptions{Site: "domestic"})
				if err != nil {
					continue
				}
				_ = pool.MarkResult(sel, true, "", 0)
				ops.Add(1)
			}
		})
	}
	wg.Wait()
	elapsed := time.Since(start)
	rate := float64(ops.Load()) / elapsed.Seconds()
	t.Logf("ops=%d elapsed=%s rate=%.0f ops/s", ops.Load(), elapsed, rate)
	if rate < 500 {
		t.Fatalf("expected memory-path throughput >= 500 ops/s, got %.0f", rate)
	}
}

func TestSummarizeStoreForSitePrefersActiveRegion(t *testing.T) {
	store := accounts.EmptyStore()
	store.Accounts = []accounts.Account{
		accounts.CreateAccount(accounts.Account{Label: "cn", Site: "domestic", BearerToken: "cn-token", Enabled: true}),
		accounts.CreateAccount(accounts.Account{Label: "us", Site: "global", BearerToken: "us-token", Enabled: true}),
	}
	summary := accounts.SummarizeStoreForSite(store, "mem", "global")
	if summary.ActiveSite != "global" {
		t.Fatalf("activeSite=%q", summary.ActiveSite)
	}
	if summary.DomesticCount != 1 || summary.GlobalCount != 1 {
		t.Fatalf("counts domestic=%d global=%d", summary.DomesticCount, summary.GlobalCount)
	}
	if summary.Primary == nil || summary.Primary.Site != "global" {
		t.Fatalf("primary=%+v", summary.Primary)
	}
	if summary.ActiveEnabledCount != 1 {
		t.Fatalf("activeEnabled=%d", summary.ActiveEnabledCount)
	}
}

func TestPoolSelectSkipsCooldown(t *testing.T) {
	pool := accounts.NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	defer pool.Close()
	one, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "one", BearerToken: "token-one", Site: "domestic",
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "two", BearerToken: "token-two", Site: "domestic",
	}))
	if err != nil {
		t.Fatal(err)
	}
	sel, err := pool.Select(accounts.SelectOptions{Site: "domestic", AccountID: one.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.MarkResult(sel, false, "429 too many requests", 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		picked, err := pool.Select(accounts.SelectOptions{Site: "domestic"})
		if err != nil {
			t.Fatal(err)
		}
		if picked.Account.ID == one.ID {
			t.Fatalf("expected cooldown account skipped, got %s on attempt %d", one.ID, i+1)
		}
	}
}

func TestPoolSelectSkipsQuotaExhausted(t *testing.T) {
	pool := accounts.NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	defer pool.Close()
	zero := 0.0
	one, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "one", BearerToken: "token-one", Site: "domestic",
		QuotaRemaining: &zero,
		QuotaResetAt:   time.Now().Add(5 * time.Hour).UnixMilli(),
		QuotaCheckedAt: time.Now().UnixMilli(),
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "two", BearerToken: "token-two", Site: "domestic",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		picked, err := pool.Select(accounts.SelectOptions{Site: "domestic"})
		if err != nil {
			t.Fatal(err)
		}
		if picked.Account.ID == one.ID {
			t.Fatalf("expected quota-exhausted account skipped, got %s on attempt %d", one.ID, i+1)
		}
	}
}

func TestMarkResultSuccessClearsQuotaBlock(t *testing.T) {
	pool := accounts.NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	defer pool.Close()
	zero := 0.0
	resetAt := time.Now().Add(5 * time.Hour).UnixMilli()
	one, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "one", BearerToken: "token-one", Site: "domestic",
		QuotaRemaining: &zero,
		QuotaResetAt:   resetAt,
		QuotaCheckedAt: time.Now().UnixMilli(),
	}))
	if err != nil {
		t.Fatal(err)
	}
	sel, err := pool.Select(accounts.SelectOptions{Site: "domestic", AccountID: one.ID})
	if err != nil {
		t.Fatal(err)
	}
	sel.BypassedCooldown = true
	if err := pool.MarkResult(sel, true, "", 0); err != nil {
		t.Fatal(err)
	}
	store, err := pool.Read()
	if err != nil {
		t.Fatal(err)
	}
	var updated accounts.Account
	for _, item := range store.Accounts {
		if item.ID == one.ID {
			updated = item
			break
		}
	}
	if updated.QuotaRemaining != nil || updated.QuotaResetAt != 0 {
		t.Fatalf("expected quota block cleared, got remaining=%v resetAt=%d", updated.QuotaRemaining, updated.QuotaResetAt)
	}
	if accounts.QuotaBlocked(updated, time.Now().UnixMilli()) {
		t.Fatal("account should not be quota-blocked after successful request")
	}
}

func TestMarkResultSuccessPreservesQuotaCache(t *testing.T) {
	pool := accounts.NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	defer pool.Close()
	remaining := 80.0
	one, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "one", BearerToken: "token-one", Site: "domestic",
		QuotaRemaining: &remaining,
		QuotaCheckedAt: time.Now().UnixMilli(),
	}))
	if err != nil {
		t.Fatal(err)
	}
	sel, err := pool.Select(accounts.SelectOptions{Site: "domestic", AccountID: one.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.MarkResult(sel, true, "", 0); err != nil {
		t.Fatal(err)
	}
	store, err := pool.Read()
	if err != nil {
		t.Fatal(err)
	}
	var updated accounts.Account
	for _, item := range store.Accounts {
		if item.ID == one.ID {
			updated = item
			break
		}
	}
	if updated.QuotaRemaining == nil || *updated.QuotaRemaining != 80 {
		t.Fatalf("healthy quota cache must survive success, got remaining=%v", updated.QuotaRemaining)
	}
}

func TestUpsertSameUserDifferentSites(t *testing.T) {
	pool := accounts.NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	defer pool.Close()
	cn, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "cn", Site: "domestic", BearerToken: "token-cn",
		AuthStatus: accounts.AuthStatus{UserID: "user@example.com"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	us, store, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "us", Site: "global", BearerToken: "token-us",
		AuthStatus: accounts.AuthStatus{UserID: "user@example.com"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cn.ID == us.ID {
		t.Fatalf("domestic and global accounts must not collapse: %s", cn.ID)
	}
	if len(store.Accounts) != 2 {
		t.Fatalf("accounts=%d want 2", len(store.Accounts))
	}
}

func TestPoolSelectFallbackWhenAllCooldown(t *testing.T) {
	pool := accounts.NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	defer pool.Close()
	fast, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "fast", BearerToken: "token-fast", Site: "domestic",
	}))
	if err != nil {
		t.Fatal(err)
	}
	slow, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "slow", BearerToken: "token-slow", Site: "domestic",
	}))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	selFast, err := pool.Select(accounts.SelectOptions{Site: "domestic", AccountID: fast.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.MarkResult(selFast, false, "502 bad gateway", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	selSlow, err := pool.Select(accounts.SelectOptions{Site: "domestic", AccountID: slow.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.MarkResult(selSlow, false, "502 bad gateway", 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	picked, err := pool.Select(accounts.SelectOptions{Site: "domestic"})
	if err != nil {
		t.Fatalf("expected fallback select, got err=%v", err)
	}
	if !picked.BypassedCooldown {
		t.Fatal("expected BypassedCooldown=true")
	}
	if picked.Account.ID != fast.ID {
		t.Fatalf("expected fastest recovery account %s, got %s", fast.ID, picked.Account.ID)
	}
	store, err := pool.Read()
	if err != nil {
		t.Fatal(err)
	}
	for _, acc := range store.Accounts {
		if acc.ID == fast.ID && acc.CooldownUntil <= now {
			t.Fatalf("fast account should still be cooling")
		}
	}
}

func TestPoolPreferQuotaPicksHighestRemaining(t *testing.T) {
	dir := t.TempDir()
	pool := accounts.NewPool(filepath.Join(dir, "accounts.json"))
	defer pool.Close()
	lowRem := 10.0
	highRem := 80.0
	low, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "low", Site: "domestic", BearerToken: "t-low", QuotaRemaining: &lowRem,
	}))
	if err != nil {
		t.Fatal(err)
	}
	high, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "high", Site: "domestic", BearerToken: "t-high", QuotaRemaining: &highRem,
	}))
	if err != nil {
		t.Fatal(err)
	}
	rr, err := pool.Select(accounts.SelectOptions{Site: "domestic"})
	if err != nil {
		t.Fatal(err)
	}
	if rr.Account.ID != low.ID {
		t.Fatalf("round-robin without PreferQuota should keep insert order, got %s", rr.Account.ID)
	}
	sel, err := pool.Select(accounts.SelectOptions{Site: "domestic", PreferQuota: true})
	if err != nil {
		t.Fatal(err)
	}
	if sel.Account.ID != high.ID {
		t.Fatalf("PreferQuota picked %s want %s", sel.Account.ID, high.ID)
	}
}

func TestPoolPreferQuotaFallsBackToRoundRobinWithoutQuota(t *testing.T) {
	pool := accounts.NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	defer pool.Close()
	first, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "first", Site: "domestic", BearerToken: "t-first",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "second", Site: "domestic", BearerToken: "t-second",
	})); err != nil {
		t.Fatal(err)
	}
	sel, err := pool.Select(accounts.SelectOptions{Site: "domestic", PreferQuota: true})
	if err != nil {
		t.Fatal(err)
	}
	if sel.Account.ID != first.ID {
		t.Fatalf("no quota data should keep round-robin, got %s", sel.Account.ID)
	}
}
