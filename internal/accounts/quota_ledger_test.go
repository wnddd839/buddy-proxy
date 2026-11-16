package accounts

import (
	"testing"
	"time"
)

// 本文件验证「本地扣费记账」。
//
// 背景（2026-11-16 线上取证）：
//   MarkResult 原本没有 credit 参数。成功请求拿到的上游扣费（Usage.Credit）
//   只写进 usage journal，从未回写 QuotaRemaining。于是余额只在轮询刷新时
//   才更新（quotaCacheMaxAge = 6 小时），账号可在两次刷新之间被打穿。
//
// 实测证据（账号 70b8cc3ac591cef7）：
//   连续 70 次请求全部成功，累计扣费 21.90，最终 rem=0。
//   若每次请求后本地扣减，余额在第 6 次左右就跌破下限 20，早该被拦住。
//
// 设计要点：
//   1. credit > 0 才扣减（22.9% 的请求扣费为 0，属免费/套餐内）
//   2. 不改 QuotaCheckedAt —— 本地扣减不等于「已向上游核实」
//   3. 累计到 QuotaSpentSinceCheck，供对账时与服务端差额比对
//   4. 扣到负数也如实记录（宁可多算，不能少算）

func newLedgerPool(t *testing.T) *Pool {
	t.Helper()
	p := NewPool("")
	p.mu.Lock()
	p.mem = Store{Accounts: []Account{
		{ID: "acc1", Label: "test", Enabled: true, Site: "global",
			QuotaRemaining: ledgerFloatPtr(100), QuotaCheckedAt: time.Now().UnixMilli()},
	}}
	p.loaded = true
	p.mu.Unlock()
	return p
}

func ledgerFloatPtr(v float64) *float64 { return &v }

func ledgerSel(id string) Selection {
	return Selection{Source: "pool", Account: Account{ID: id}}
}

func ledgerAccount(p *Pool, id string) Account {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, a := range p.mem.Accounts {
		if a.ID == id {
			return a
		}
	}
	return Account{}
}

// TestMarkResultDeductsCredit 校验成功请求按 credit 扣减余额。
func TestMarkResultDeductsCredit(t *testing.T) {
	p := newLedgerPool(t)
	if err := p.MarkResult(ledgerSel("acc1"), true, "", 0, 0.25); err != nil {
		t.Fatalf("MarkResult: %v", err)
	}
	a := ledgerAccount(p, "acc1")
	if a.QuotaRemaining == nil {
		t.Fatal("QuotaRemaining 不应被清空")
	}
	if got := *a.QuotaRemaining; got < 99.74 || got > 99.76 {
		t.Fatalf("应扣到 99.75，实际 %.4f", got)
	}
	if a.QuotaSpentSinceCheck < 0.24 || a.QuotaSpentSinceCheck > 0.26 {
		t.Fatalf("累计消耗应约 0.25，实际 %.4f", a.QuotaSpentSinceCheck)
	}
}

// TestMarkResultAccumulatesCredit 校验多次请求累加（用真实线上数据）。
func TestMarkResultAccumulatesCredit(t *testing.T) {
	p := newLedgerPool(t)
	for _, c := range []float64{4.03, 0.73, 0.26, 0.25} {
		if err := p.MarkResult(ledgerSel("acc1"), true, "", 0, c); err != nil {
			t.Fatalf("MarkResult: %v", err)
		}
	}
	a := ledgerAccount(p, "acc1")
	want := 100.0 - (4.03 + 0.73 + 0.26 + 0.25)
	if got := *a.QuotaRemaining; got < want-0.01 || got > want+0.01 {
		t.Fatalf("应扣到 %.4f，实际 %.4f", want, got)
	}
	if a.QuotaSpentSinceCheck < 5.26 || a.QuotaSpentSinceCheck > 5.28 {
		t.Fatalf("累计应约 5.27，实际 %.4f", a.QuotaSpentSinceCheck)
	}
}

// TestMarkResultIgnoresZeroCredit 校验 credit=0 时不扣减也不改累计。
func TestMarkResultIgnoresZeroCredit(t *testing.T) {
	p := newLedgerPool(t)
	if err := p.MarkResult(ledgerSel("acc1"), true, "", 0, 0); err != nil {
		t.Fatalf("MarkResult: %v", err)
	}
	a := ledgerAccount(p, "acc1")
	if got := *a.QuotaRemaining; got != 100 {
		t.Fatalf("credit=0 不应扣减，实际 %.4f", got)
	}
	if a.QuotaSpentSinceCheck != 0 {
		t.Fatalf("累计应仍为 0，实际 %.4f", a.QuotaSpentSinceCheck)
	}
}

// TestMarkResultDoesNotTouchCheckedAt 是关键约束：
// 本地扣减不等于「向上游核实过」，QuotaCheckedAt 必须保持不变。
func TestMarkResultDoesNotTouchCheckedAt(t *testing.T) {
	p := newLedgerPool(t)
	before := ledgerAccount(p, "acc1").QuotaCheckedAt
	if err := p.MarkResult(ledgerSel("acc1"), true, "", 0, 1.5); err != nil {
		t.Fatalf("MarkResult: %v", err)
	}
	after := ledgerAccount(p, "acc1").QuotaCheckedAt
	if after != before {
		t.Fatalf("QuotaCheckedAt 不得改动：before=%d after=%d", before, after)
	}
}

// TestMarkResultFailureDoesNotDeduct 校验失败请求不扣费。
func TestMarkResultFailureDoesNotDeduct(t *testing.T) {
	p := newLedgerPool(t)
	if err := p.MarkResult(ledgerSel("acc1"), false, "boom", time.Minute, 5.0); err != nil {
		t.Fatalf("MarkResult: %v", err)
	}
	a := ledgerAccount(p, "acc1")
	if got := *a.QuotaRemaining; got != 100 {
		t.Fatalf("失败不应扣费，实际 %.4f", got)
	}
	if a.QuotaSpentSinceCheck != 0 {
		t.Fatalf("失败不应累计，实际 %.4f", a.QuotaSpentSinceCheck)
	}
}

// TestMarkResultDeductsBelowZero 校验扣到负数时如实记录（宁可多算，不能少算）。
func TestMarkResultDeductsBelowZero(t *testing.T) {
	p := newLedgerPool(t)
	if err := p.MarkResult(ledgerSel("acc1"), true, "", 0, 150); err != nil {
		t.Fatalf("MarkResult: %v", err)
	}
	a := ledgerAccount(p, "acc1")
	if got := *a.QuotaRemaining; got > -49.9 || got < -50.1 {
		t.Fatalf("应扣到 -50（如实记录负值），实际 %.4f", got)
	}
}

// TestMarkResultUnlimitedSkipsDeduction 校验不限量账号不扣减。
func TestMarkResultUnlimitedSkipsDeduction(t *testing.T) {
	p := newLedgerPool(t)
	p.mu.Lock()
	p.mem.Accounts[0].QuotaUnlimited = true
	p.mu.Unlock()
	if err := p.MarkResult(ledgerSel("acc1"), true, "", 0, 5); err != nil {
		t.Fatalf("MarkResult: %v", err)
	}
	if got := *ledgerAccount(p, "acc1").QuotaRemaining; got != 100 {
		t.Fatalf("不限量账号不应扣减，实际 %.4f", got)
	}
}

// TestMarkResultNilQuotaSkipsDeduction 校验无余额快照时不扣减，
// 也不凭空造一个值。
func TestMarkResultNilQuotaSkipsDeduction(t *testing.T) {
	p := newLedgerPool(t)
	p.mu.Lock()
	p.mem.Accounts[0].QuotaRemaining = nil
	p.mu.Unlock()
	if err := p.MarkResult(ledgerSel("acc1"), true, "", 0, 5); err != nil {
		t.Fatalf("MarkResult: %v", err)
	}
	if a := ledgerAccount(p, "acc1"); a.QuotaRemaining != nil {
		t.Fatal("无快照时不应凭空创建余额")
	}
}

// TestQuotaBelowFloorSeesLocalDeduction 是端到端关键断言：
// 本地扣减后，QuotaBelowFloor 必须立刻看到新余额并拒绝，
// **不需要等 QuotaCheckedAt 刷新**。
func TestQuotaBelowFloorSeesLocalDeduction(t *testing.T) {
	p := newLedgerPool(t)
	p.SetQuotaFloor(20)

	if QuotaBelowFloor(ledgerAccount(p, "acc1"), time.Now().UnixMilli(), 20) {
		t.Fatal("初始 100 不应低于下限 20")
	}

	if err := p.MarkResult(ledgerSel("acc1"), true, "", 0, 85); err != nil {
		t.Fatalf("MarkResult: %v", err)
	}
	a := ledgerAccount(p, "acc1")
	if !QuotaBelowFloor(a, time.Now().UnixMilli(), 20) {
		t.Fatalf("扣到 %.2f 后应立即低于下限 20", *a.QuotaRemaining)
	}
}

// TestApplyQuotaStateResetsLocalSpent 校验上游对账后清零本地累计
// （对账即以服务端为准，本地累计重新开始）。
func TestApplyQuotaStateResetsLocalSpent(t *testing.T) {
	p := newLedgerPool(t)
	if err := p.MarkResult(ledgerSel("acc1"), true, "", 0, 30); err != nil {
		t.Fatalf("MarkResult: %v", err)
	}
	if a := ledgerAccount(p, "acc1"); a.QuotaSpentSinceCheck == 0 {
		t.Fatal("前置条件：应已有本地累计")
	}

	if _, err := p.ApplyQuotaState("acc1", ledgerFloatPtr(200), false, 0, time.Now().UnixMilli()); err != nil {
		t.Fatalf("ApplyQuotaState: %v", err)
	}
	a := ledgerAccount(p, "acc1")
	if *a.QuotaRemaining != 200 {
		t.Fatalf("应以服务端 200 覆盖，实际 %.2f", *a.QuotaRemaining)
	}
	if a.QuotaSpentSinceCheck != 0 {
		t.Fatalf("对账后本地累计应清零，实际 %.4f", a.QuotaSpentSinceCheck)
	}
}
