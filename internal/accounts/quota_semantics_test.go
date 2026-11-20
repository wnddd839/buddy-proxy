package accounts

import (
	"testing"
	"time"
)

// 额度守卫修复后的行为矩阵。
//
// 设计不变量（PR66 已明确）：
//   - 额度未知（nil）→ 放行。新导入的号还没探测过，拦下就永远用不了。
//   - 余额 < floor 且数据新鲜 → 拦住。
//   - 数据陈旧（>6h）→ 放行，靠探测重建（"未知不等于低余额"）。
//   - 已过重置时间 → 放行。
//
// 保护写在**写入侧**而非判断侧：
//   - 修 B：免费模型（credit=0）的成功不清空额度快照
//   - 修 C：额度耗尽的号不进入「全冷却兜底」候选
func TestQuotaFloorSemantics(t *testing.T) {
	now := time.Now().UnixMilli()
	future := now + 3600_000
	rem0 := 0.0
	rem5 := 5.0
	rem10 := 10.0
	rem100 := 100.0

	cases := []struct {
		name string
		acc  Account
		want bool // QuotaBelowFloor(floor=10) 的期望值
	}{
		{
			name: "nil（未知）→ 放行：新号要能跑",
			acc:  Account{QuotaRemaining: nil},
			want: false,
		},
		{
			name: "余额 0 + 新鲜 → 拦住",
			acc:  Account{QuotaRemaining: &rem0, QuotaCheckedAt: now, QuotaResetAt: future},
			want: true,
		},
		{
			name: "余额 5 < 10 → 拦住",
			acc:  Account{QuotaRemaining: &rem5, QuotaCheckedAt: now, QuotaResetAt: future},
			want: true,
		},
		{
			name: "余额恰好 10（== floor）→ 拦住（判据是 < floor，10 不小于 10 应放行）",
			acc:  Account{QuotaRemaining: &rem10, QuotaCheckedAt: now, QuotaResetAt: future},
			want: false, // 10 < 10 = false
		},
		{
			name: "余额 100 > 10 → 放行",
			acc:  Account{QuotaRemaining: &rem100, QuotaCheckedAt: now, QuotaResetAt: future},
			want: false,
		},
		{
			name: "余额 0 但 checkedAt 陈旧(>6h) → 放行（未知不等于低余额）",
			acc:  Account{QuotaRemaining: &rem0, QuotaCheckedAt: now - 7*3600_000, QuotaResetAt: future},
			want: false,
		},
		{
			name: "余额 0 但重置时间已过 → 放行",
			acc:  Account{QuotaRemaining: &rem0, QuotaCheckedAt: now, QuotaResetAt: now - 1000},
			want: false,
		},
		{
			name: "QuotaUnlimited → 永远放行",
			acc:  Account{QuotaRemaining: &rem0, QuotaUnlimited: true},
			want: false,
		},
	}
	for _, c := range cases {
		got := QuotaBelowFloor(c.acc, now, 10)
		if got != c.want {
			t.Errorf("%s: QuotaBelowFloor=%v 期望 %v", c.name, got, c.want)
		} else {
			t.Logf("✓ %s → %v", c.name, got)
		}
	}
}

// 🔴 修 B 的核心断言：免费模型（credit=0）成功**不能**清空额度快照。
//
// 修复前的行为：`BypassedCooldown && wasQuotaBlocked` 就清空（不看 credit）。
// 于是「额度耗尽号 + 免费模型成功」= 清空 = 变 nil = 放行 + 停止记账。
// 免费模型就是那把钥匙。
func TestFreeModelSuccessDoesNotClearQuota(t *testing.T) {
	p := NewPool(t.TempDir() + "/accounts.json")
	defer p.Close()
	zero := 0.0
	now := time.Now().UnixMilli()
	acc, _, err := p.Upsert(CreateAccount(Account{
		Label: "g", BearerToken: "t", Site: "global",
		QuotaRemaining: &zero,
		QuotaResetAt:   now + 3600_000,
		QuotaCheckedAt: now,
	}))
	if err != nil {
		t.Fatal(err)
	}
	// 直接构造一个「兜底选中」的 Selection，模拟全冷却降级的场景
	sel := Selection{
		Source:           "pool",
		Account:          acc,
		BypassedCooldown: true,
		Model:            "hy4flash-free",
	}
	if err := p.MarkResult(sel, true, "", 0, 0); err != nil { // credit=0 = 免费模型
		t.Fatal(err)
	}
	for _, a := range p.mem.Accounts {
		if a.ID != acc.ID {
			continue
		}
		if a.QuotaRemaining == nil {
			t.Fatal("免费模型成功清空了额度快照 —— 会开一条永久免费通道（不拦也不记账）")
		}
		if *a.QuotaRemaining != 0 {
			t.Fatalf("额度应保持 0，实际 %v", *a.QuotaRemaining)
		}
		if !QuotaBlocked(a, time.Now().UnixMilli()) {
			t.Error("免费模型成功后仍应判为额度阻塞")
		}
		t.Log("✓ 免费模型成功后额度快照与阻塞状态都保留")
	}
}

// 对照：真实扣费（credit>0）成功 + 兜底 + 原本阻塞 ⇒ 清空（原语义保留）。
func TestPaidSuccessStillClearsQuotaWhenBypassed(t *testing.T) {
	p := NewPool(t.TempDir() + "/accounts.json")
	defer p.Close()
	zero := 0.0
	now := time.Now().UnixMilli()
	acc, _, err := p.Upsert(CreateAccount(Account{
		Label: "g", BearerToken: "t", Site: "global",
		QuotaRemaining: &zero,
		QuotaResetAt:   now + 3600_000,
		QuotaCheckedAt: now,
	}))
	if err != nil {
		t.Fatal(err)
	}
	sel := Selection{Source: "pool", Account: acc, BypassedCooldown: true, Model: "m"}
	if err := p.MarkResult(sel, true, "", 0, 1.0); err != nil { // credit>0 = 真实扣费
		t.Fatal(err)
	}
	for _, a := range p.mem.Accounts {
		if a.ID != acc.ID {
			continue
		}
		if a.QuotaRemaining != nil {
			t.Fatalf("真实扣费成功后应清空过期快照，实际 %v", *a.QuotaRemaining)
		}
		t.Log("✓ 真实扣费成功后清空过期快照（原语义保留）")
	}
}

// 🔴 修 C 的核心断言：额度耗尽的号不进入「全冷却兜底」。
//
// 修复前：它会被 fallback 选中（BypassedCooldown），上游回 429 或被免费模型
// 打成功。前者白烧一次，后者触发清空。
func TestQuotaBlockedNotUsedAsCooldownFallback(t *testing.T) {
	p := NewPool(t.TempDir() + "/accounts.json")
	defer p.Close()
	zero := 0.0
	now := time.Now().UnixMilli()
	// 额度耗尽 + 冷却中（两个条件都满足最坏情况）
	if _, _, err := p.Upsert(CreateAccount(Account{
		Label: "exhausted", BearerToken: "t", Site: "global",
		QuotaRemaining: &zero,
		QuotaResetAt:   now + 3600_000,
		QuotaCheckedAt: now,
		CooldownUntil:  now + 3600_000,
		CooldownSource: CooldownSourceQuota,
	})); err != nil {
		t.Fatal(err)
	}
	p.SetQuotaFloor(20)
	sel, err := p.Select(SelectOptions{Site: "global", Model: "m", CheckCooldown: true})
	if err == nil {
		t.Fatalf("额度耗尽的号被选中了（bypassed=%v）—— 应直接拒绝", sel.BypassedCooldown)
	}
	t.Logf("✓ 额度耗尽账号被拒绝: %v", err)
}

// 对照：仅 upstream 冷却、余额充足 ⇒ 仍应走兜底（不能把正常兜底也砍了）。
func TestUpstreamCooldownStillFallsBack(t *testing.T) {
	p := NewPool(t.TempDir() + "/accounts.json")
	defer p.Close()
	rich := 300.0
	now := time.Now().UnixMilli()
	if _, _, err := p.Upsert(CreateAccount(Account{
		Label: "rich", BearerToken: "t", Site: "global",
		QuotaRemaining: &rich,
		QuotaCheckedAt: now,
		CooldownUntil:  now + 60_000, // 仅 upstream 冷却
		CooldownSource: CooldownSourceUpstream,
	})); err != nil {
		t.Fatal(err)
	}
	p.SetQuotaFloor(20)
	sel, err := p.Select(SelectOptions{Site: "global", Model: "m", CheckCooldown: true})
	if err != nil {
		t.Fatalf("余额充足的 upstream 冷却号应能兜底，实际被拒: %v", err)
	}
	if !sel.BypassedCooldown {
		t.Error("应标记 BypassedCooldown")
	}
	t.Log("✓ 余额充足的 upstream 冷却号仍走兜底（正常路径未被破坏）")
}
