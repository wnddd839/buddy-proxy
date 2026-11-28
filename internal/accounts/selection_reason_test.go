package accounts

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// 选号必须交代每个被检查过却没选的号为什么没选。
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
	// candidates 是池内账号总数（与 skipped 的统计域一致）
	if got.Candidates != 3 {
		t.Fatalf("Candidates 应为 3，实际 %d", got.Candidates)
	}
	// examined 是本次真正检查过的号数
	if got.Examined < 1 || got.Examined > 3 {
		t.Fatalf("Examined 超出范围：%d", got.Examined)
	}
	// 每个被检查过但没选的号都要有原因
	if sum := totalSkipped(got.Skipped); sum != got.Examined-1 {
		t.Fatalf("跳过原因(%d)应覆盖 examined-1(%d)：%v", sum, got.Examined-1, got.Skipped)
	}
	if got.Skipped[SkipQuotaFloor] == 0 && got.Skipped[SkipCoolingDown] == 0 {
		t.Fatalf("额度不足或冷却应至少命中一个：%v", got.Skipped)
	}
}

func totalSkipped(m map[string]int) int {
	sum := 0
	for _, v := range m {
		sum += v
	}
	return sum
}

// 全池不可选时：事件必须发（Chosen 空、原因覆盖被检查的号），
// 且错误里必须带计数，让「没配账号」与「全被拦住」能区分。
func TestNoAccountsErrorCarriesCounts(t *testing.T) {
	pool := newObserverPool(t)
	low := 1.0
	for _, tok := range []string{"t1", "t2"} {
		acc := CreateAccount(Account{
			Label: "low" + tok, BearerToken: tok, Site: "global",
			QuotaRemaining: &low, QuotaCheckedAt: time.Now().UnixMilli(),
			QuotaResetAt: time.Now().Add(time.Hour).UnixMilli(),
		})
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
	if totalSkipped(got.Skipped) != got.Examined {
		t.Fatalf("原因总数(%d)应等于 examined(%d)：%v", totalSkipped(got.Skipped), got.Examined, got.Skipped)
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

// 跨站点的号也必须被交代（原因里出现 site_mismatch），
// 因为 candidates 用的是全部账号数，两者统计域必须一致。
func TestCrossSiteAccountsAreExplained(t *testing.T) {
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
	if got.Candidates != 1 {
		t.Fatalf("Candidates 应为全部账号数 1，实际 %d", got.Candidates)
	}
	if got.Skipped[SkipSiteMismatch] != 1 {
		t.Fatalf("跨站点的号应记为 site_mismatch：%v", got.Skipped)
	}
	if totalSkipped(got.Skipped) != got.Examined {
		t.Fatalf("原因总数(%d)应等于 examined(%d)", totalSkipped(got.Skipped), got.Examined)
	}
}

// 健康池的第一个号就可选 ⇒ 只检查了 1 个，没有任何跳过原因。
// 这是 I7 旧判据会稳定误报的场景（评审 C1）。
func TestHealthyFirstPickHasNoSkips(t *testing.T) {
	pool := newObserverPool(t)
	for _, tok := range []string{"t1", "t2", "t3"} {
		acc := CreateAccount(Account{Label: "ok" + tok, BearerToken: tok, Site: "global"})
		if _, _, err := pool.Upsert(acc); err != nil {
			t.Fatal(err)
		}
	}
	obs := &fakeObserver{}
	pool.SetObserver(obs)

	if _, err := pool.Select(SelectOptions{Site: "global", Model: "m", RequestID: "r"}); err != nil {
		t.Fatal(err)
	}
	got := obs.selection[0]
	if got.Examined != 1 {
		t.Fatalf("轮转下应只检查 1 个号，实际 %d", got.Examined)
	}
	if totalSkipped(got.Skipped) != 0 {
		t.Fatalf("没有跳过不该产生原因：%v", got.Skipped)
	}
}
