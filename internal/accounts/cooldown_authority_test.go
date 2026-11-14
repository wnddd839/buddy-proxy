package accounts_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
)

// MarkResult 收到额度类失败时，绝不能把 ApplyQuotaState 刚标好的 quota 冷却
// 降级成 upstream。旧实现无条件覆盖，导致「额度冷却自愈」在真实 429 路径上
// 完全失效——号会一直冷却到 resetAt，而且再也不会被额度快照救回来。
func TestMarkResultKeepsQuotaCooldownSource(t *testing.T) {
	pool := accounts.NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	t.Cleanup(func() { _ = pool.Close() })
	created, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "quota", Site: "domestic", BearerToken: "eyJhbGciOiJIUzI1NiJ9.e30.quota",
	}))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	reset := now.Add(48 * time.Hour)

	selection, err := pool.Select(accounts.SelectOptions{Site: "domestic"})
	if err != nil {
		t.Fatal(err)
	}
	// ① 额度探测先写：余额 0、冷却到 resetAt、来源 quota
	if _, err := pool.ApplyQuotaState(created.ID, remainingPtr(0), false, reset.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	// ② 紧接着 MarkResult 报失败，cooldown 正好等于到 resetAt 的剩余时长
	if err := pool.MarkResult(selection, false, "429 usage exceeds frequency limit (code 6004)",
		reset.Sub(now)); err != nil {
		t.Fatal(err)
	}

	store, err := pool.Read()
	if err != nil {
		t.Fatal(err)
	}
	got := store.Accounts[0]
	if got.CooldownSource != accounts.CooldownSourceQuota {
		t.Fatalf("额度冷却被 MarkResult 降级成 %q，自愈会失效", got.CooldownSource)
	}

	// ③ 余额被上游补回来 ⇒ 必须能自愈
	recovered, err := pool.ApplyQuotaState(created.ID, remainingPtr(425), false, reset.UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if recovered.CooldownUntil != 0 {
		t.Fatalf("余额恢复后冷却应清零，实际 %d", recovered.CooldownUntil)
	}
	if accounts.InCooldown(recovered, now.UnixMilli()) {
		t.Fatal("账号应已可选中")
	}
}

// 真正的限流（非额度）仍然必须写 upstream，不能被误判成可自愈。
func TestMarkResultWritesUpstreamSourceForRateLimit(t *testing.T) {
	pool := accounts.NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	t.Cleanup(func() { _ = pool.Close() })
	if _, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "rl", Site: "domestic", BearerToken: "eyJhbGciOiJIUzI1NiJ9.e30.rl",
	})); err != nil {
		t.Fatal(err)
	}
	selection, err := pool.Select(accounts.SelectOptions{Site: "domestic"})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.MarkResult(selection, false, "502 bad gateway", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	store, err := pool.Read()
	if err != nil {
		t.Fatal(err)
	}
	if store.Accounts[0].CooldownSource != accounts.CooldownSourceUpstream {
		t.Fatalf("非额度失败应写 upstream，实际 %q", store.Accounts[0].CooldownSource)
	}
}

// UsableForModel 是唯一权威判据：四项条件缺一不可。
func TestUsableForModelCoversAllFourConditions(t *testing.T) {
	now := time.Now().UnixMilli()
	base := func() accounts.Account {
		remaining := 100.0
		return accounts.Account{
			ID: "a", Enabled: true, Site: "domestic",
			BearerToken:    "eyJhbGciOiJIUzI1NiJ9.e30.u",
			QuotaRemaining: &remaining, QuotaCheckedAt: now,
		}
	}

	if !accounts.UsableForModel(base(), now, "m", 20) {
		t.Fatal("健康账号应可用")
	}

	disabled := base()
	disabled.Enabled = false
	if accounts.UsableForModel(disabled, now, "m", 20) {
		t.Fatal("停用账号不可用")
	}

	noCreds := base()
	noCreds.BearerToken = ""
	if accounts.UsableForModel(noCreds, now, "m", 20) {
		t.Fatal("无凭据账号不可用")
	}

	cooling := base()
	cooling.CooldownUntil = now + 60_000
	if accounts.UsableForModel(cooling, now, "m", 20) {
		t.Fatal("冷却中不可用")
	}

	modelCool := base()
	modelCool.ModelCooldowns = map[string]int64{"m": now + 60_000}
	if accounts.UsableForModel(modelCool, now, "m", 20) {
		t.Fatal("模型级冷却不可用")
	}
	if !accounts.UsableForModel(modelCool, now, "other", 20) {
		t.Fatal("其它模型不受该模型冷却影响")
	}

	belowFloor := base()
	low := 5.0
	belowFloor.QuotaRemaining = &low
	belowFloor.QuotaResetAt = now + 3_600_000
	if accounts.UsableForModel(belowFloor, now, "m", 20) {
		t.Fatal("低于积分下限不可用")
	}
}
