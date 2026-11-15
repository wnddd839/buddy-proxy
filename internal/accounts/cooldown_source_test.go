package accounts_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
)

func newCooldownPool(t *testing.T) (*accounts.Pool, string) {
	t.Helper()
	pool := accounts.NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	t.Cleanup(func() { _ = pool.Close() })
	id, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label:       "cooling",
		Site:        "domestic",
		BearerToken: "eyJhbGciOiJIUzI1NiJ9.e30.cooling",
	}))
	if err != nil {
		t.Fatal(err)
	}
	return pool, id.ID
}

func remainingPtr(v float64) *float64 { return &v }

// 额度耗尽写冷却时必须标 quota —— 否则余额恢复后没人敢清它。
func TestApplyQuotaStateMarksQuotaCooldown(t *testing.T) {
	pool, id := newCooldownPool(t)
	now := time.Now().UnixMilli()
	reset := now + 48*time.Hour.Milliseconds()

	got, err := pool.ApplyQuotaState(id, remainingPtr(0), false, reset, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.CooldownUntil != reset {
		t.Fatalf("冷却应设到 resetAt=%d，实际 %d", reset, got.CooldownUntil)
	}
	if got.CooldownSource != accounts.CooldownSourceQuota {
		t.Fatalf("冷却来源应为 quota，实际 %q", got.CooldownSource)
	}
}

// 上游提前补回额度 ⇒ 冷却自动解除（本次事故的直接修复）。
func TestQuotaCooldownSelfHealsWhenBalanceRecovers(t *testing.T) {
	pool, id := newCooldownPool(t)
	now := time.Now().UnixMilli()
	reset := now + 96*time.Hour.Milliseconds()

	if _, err := pool.ApplyQuotaState(id, remainingPtr(0), false, reset, now); err != nil {
		t.Fatal(err)
	}
	got, err := pool.ApplyQuotaState(id, remainingPtr(425), false, reset, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.CooldownUntil != 0 {
		t.Fatalf("余额恢复后冷却应清零，实际 %d", got.CooldownUntil)
	}
	if got.CooldownSource != "" {
		t.Fatalf("冷却来源应清空，实际 %q", got.CooldownSource)
	}
	if accounts.InCooldown(got, now) {
		t.Fatal("账号应已回到可选中状态")
	}
}

// 真实限流（upstream）不是额度问题，余额恢复不能顺手把它清掉。
func TestUpstreamCooldownSurvivesQuotaRecovery(t *testing.T) {
	pool, _ := newCooldownPool(t)
	selection, err := pool.Select(accounts.SelectOptions{Site: "domestic"})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.MarkResult(selection, false, "429 too many requests", 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	got, err := pool.ApplyQuotaState(selection.Account.ID, remainingPtr(500), false, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.CooldownSource != accounts.CooldownSourceUpstream {
		t.Fatalf("冷却来源应为 upstream，实际 %q", got.CooldownSource)
	}
	if got.CooldownUntil <= now {
		t.Fatalf("限流冷却不该被额度快照清掉，实际直到 %d（now=%d）", got.CooldownUntil, now)
	}
}

// 没有余额证据（remaining=nil）时不许猜「已恢复」。
func TestQuotaCooldownKeptWithoutBalanceEvidence(t *testing.T) {
	pool, id := newCooldownPool(t)
	now := time.Now().UnixMilli()
	reset := now + 12*time.Hour.Milliseconds()

	if _, err := pool.ApplyQuotaState(id, remainingPtr(0), false, reset, now); err != nil {
		t.Fatal(err)
	}
	got, err := pool.ApplyQuotaState(id, nil, false, reset, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.CooldownUntil != reset {
		t.Fatalf("没有余额证据时应保留冷却，实际 %d", got.CooldownUntil)
	}
	if got.CooldownSource != accounts.CooldownSourceQuota {
		t.Fatalf("来源应保持 quota，实际 %q", got.CooldownSource)
	}
}

// 成功一次就清冷却，来源字段也必须一起清，否则会留下「来源是 quota 但没冷却」的脏状态。
func TestMarkResultSuccessClearsCooldownSource(t *testing.T) {
	pool, _ := newCooldownPool(t)
	selection, err := pool.Select(accounts.SelectOptions{Site: "domestic"})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.MarkResult(selection, false, "429", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := pool.MarkResult(selection, true, "", 0); err != nil {
		t.Fatal(err)
	}
	store, err := pool.Read()
	if err != nil {
		t.Fatal(err)
	}
	if store.Accounts[0].CooldownUntil != 0 || store.Accounts[0].CooldownSource != "" {
		t.Fatalf("成功应清空冷却与来源，实际 until=%d source=%q",
			store.Accounts[0].CooldownUntil, store.Accounts[0].CooldownSource)
	}
}

// 加字段之前的旧数据：CooldownUntil 与 QuotaResetAt 精确相等 ⇒ 认成额度冷却。
// 这正是本次两个「余额 425/461 却冷却到 11-18」的号的特征。
func TestNormalizeCooldownSourceClassifiesLegacyData(t *testing.T) {
	const token = "eyJhbGciOiJIUzI1NiJ9.e30.legacy"
	reset := int64(1794975648000)

	quota := accounts.NormalizeAccount(accounts.Account{
		BearerToken: token, Site: "domestic",
		CooldownUntil: reset, QuotaResetAt: reset,
	}, 1)
	if quota.CooldownSource != accounts.CooldownSourceQuota {
		t.Fatalf("旧额度冷却应认成 quota，实际 %q", quota.CooldownSource)
	}

	upstream := accounts.NormalizeAccount(accounts.Account{
		BearerToken: token, Site: "domestic",
		CooldownUntil: reset, QuotaResetAt: reset - 60_000,
	}, 1)
	if upstream.CooldownSource != accounts.CooldownSourceUpstream {
		t.Fatalf("对不上的旧冷却应认成 upstream，实际 %q", upstream.CooldownSource)
	}

	free := accounts.NormalizeAccount(accounts.Account{BearerToken: token, Site: "domestic"}, 1)
	if free.CooldownSource != "" {
		t.Fatalf("没冷却时来源应为空，实际 %q", free.CooldownSource)
	}
}
