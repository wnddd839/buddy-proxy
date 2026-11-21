package accounts

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// 选号必须交代每个看过却没选的号为什么没选。
func TestSelectionReportsSkipReasons(t *testing.T) {
	pool := newObserverPool(t)

	low := 1.0
	future := time.Now().Add(time.Hour).UnixMilli()
	a := CreateAccount(Account{
		Label: "low", BearerToken: "t1", Site: "global",
		QuotaRemaining: &low, QuotaCheckedAt: time.Now().UnixMilli(), QuotaResetAt: future,
	})
	b := CreateAccount(Account{
		Label: "cool", BearerToken: "t2", Site: "global",
		CooldownUntil: future, CooldownSource: CooldownSourceUpstream,
	})
	c := CreateAccount(Account{Label: "ok", BearerToken: "t3", Site: "global"})
	for _, acc := range []Account{a, b, c} {
		if _, _, err := pool.Upsert(acc); err != nil {
			t.Fatal(err)
		}
	}
	pool.SetQuotaFloor(20)

	obs := &fakeObserver{}
	pool.SetObserver(obs)

	sel, err := pool.Select(SelectOptions{Site: "global", Model: "m", RequestID: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if sel.Account.Label != "ok" {
		t.Fatalf("应选中可用号：%+v", sel.Account)
	}

	if len(obs.selection) != 1 {
		t.Fatalf("应发 1 条选号事件，实际 %d", len(obs.selection))
	}
	got := obs.selection[0]
	if got.RequestID != "r1" || got.Chosen != sel.Account.ID {
		t.Fatalf("%+v", got)
	}
	if got.Candidates != 3 {
		t.Fatalf("候选数应为 3，实际 %d", got.Candidates)
	}
	if got.Skipped[SkipQuotaFloor] != 1 {
		t.Fatalf("额度不足没记到：%v", got.Skipped)
	}
	// 冷却号会先被当作兜底候选（fallbackIdx），仍然算「跳过」。
	if got.Skipped[SkipCoolingDown] != 1 {
		t.Fatalf("冷却没记到：%v", got.Skipped)
	}
}

// 全池不可选时：事件必须发（Chosen 空、直方图总和=候选数），
// 且错误里必须带计数，让「没配账号」与「全被拦住」能区分。
func TestNoAccountsErrorCarriesCounts(t *testing.T) {
	pool := newObserverPool(t)
	low := 1.0
	for i, tok := range []string{"t1", "t2"} {
		acc := CreateAccount(Account{
			Label: "low" + tok, BearerToken: tok, Site: "global",
			QuotaRemaining: &low, QuotaCheckedAt: time.Now().UnixMilli(),
			QuotaResetAt: time.Now().Add(time.Hour).UnixMilli(),
		})
		_ = i
		if _, _, err := pool.Upsert(acc); err != nil {
			t.Fatal(err)
		}
	}
	pool.SetQuotaFloor(20)

	obs := &fakeObserver{}
	pool.SetObserver(obs)

	_, err := pool.Select(SelectOptions{Site: "global", Model: "m", RequestID: "r9"})
	if !errors.Is(err, ErrNoAccounts) {
		t.Fatalf("仍须满足 errors.Is(ErrNoAccounts)：%v", err)
	}
	if !strings.Contains(err.Error(), "quota_floor=2") {
		t.Fatalf("错误里应带计数：%v", err)
	}
	if !strings.Contains(err.Error(), "candidates=2") {
		t.Fatalf("错误里应带候选数：%v", err)
	}

	if len(obs.selection) != 1 {
		t.Fatalf("失败也要发事件，实际 %d", len(obs.selection))
	}
	got := obs.selection[0]
	if got.Chosen != "" {
		t.Fatalf("失败时 Chosen 应为空：%+v", got)
	}
	sum := 0
	for _, v := range got.Skipped {
		sum += v
	}
	if sum != got.Candidates {
		t.Fatalf("直方图总和(%d)应等于候选数(%d)：%v", sum, got.Candidates, got.Skipped)
	}
}

// 「真没配账号」与「全被拦住」必须给出不同的话。
func TestEmptyPoolErrorDiffers(t *testing.T) {
	pool := newObserverPool(t)
	_, err := pool.Select(SelectOptions{Site: "global", Model: "m"})
	if !errors.Is(err, ErrNoAccounts) {
		t.Fatalf("应满足 errors.Is：%v", err)
	}
	if strings.Contains(err.Error(), "quota_floor") {
		t.Fatalf("空池不该出现跳过原因：%v", err)
	}
	if !strings.Contains(err.Error(), "candidates=0") {
		t.Fatalf("空池应报 candidates=0：%v", err)
	}
}

// 站点隔离的号不得被计入候选（否则 I7 的「总和=候选数」会误报）。
func TestCandidatesExcludeOtherSites(t *testing.T) {
	pool := newObserverPool(t)
	low := 1.0
	dom := CreateAccount(Account{
		Label: "dom", BearerToken: "t1", Site: "domestic",
		QuotaRemaining: &low, QuotaCheckedAt: time.Now().UnixMilli(),
		QuotaResetAt: time.Now().Add(time.Hour).UnixMilli(),
	})
	if _, _, err := pool.Upsert(dom); err != nil {
		t.Fatal(err)
	}
	pool.SetQuotaFloor(20)

	obs := &fakeObserver{}
	pool.SetObserver(obs)

	_, err := pool.Select(SelectOptions{Site: "global", Model: "m"})
	if err == nil {
		t.Fatal("global 站点没有号，应失败")
	}
	got := obs.selection[0]
	if got.Candidates != 0 {
		t.Fatalf("别的站点的号不该算候选：%+v", got)
	}
}
