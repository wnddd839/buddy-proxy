package accounts

import (
	"path/filepath"
	"testing"
	"time"
)

type fakeObserver struct {
	quota     []QuotaEvent
	selection []SelectionEvent
}

func (f *fakeObserver) ObserveQuota(e QuotaEvent)         { f.quota = append(f.quota, e) }
func (f *fakeObserver) ObserveSelection(e SelectionEvent) { f.selection = append(f.selection, e) }

func newObserverPool(t *testing.T) *Pool {
	t.Helper()
	pool := NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	t.Cleanup(func() { pool.Close() })
	return pool
}

func TestDeductEmitsQuotaEvent(t *testing.T) {
	pool := newObserverPool(t)
	remaining := 100.0
	acc := CreateAccount(Account{
		Label: "one", BearerToken: "tok", Site: "domestic", QuotaRemaining: &remaining,
	})
	if _, _, err := pool.Upsert(acc); err != nil {
		t.Fatal(err)
	}

	obs := &fakeObserver{}
	pool.SetObserver(obs)

	sel, err := pool.Select(SelectOptions{Site: "domestic", RequestID: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if sel.RequestID != "r1" {
		t.Fatalf("Selection 丢了 RequestID：%+v", sel)
	}
	if err := pool.MarkResult(sel, true, "", 0, 1.3); err != nil {
		t.Fatal(err)
	}

	if len(obs.quota) != 1 {
		t.Fatalf("应发 1 条额度事件，实际 %d", len(obs.quota))
	}
	e := obs.quota[0]
	if e.Op != QuotaOpDeduct || e.Credit != 1.3 || e.RequestID != "r1" {
		t.Fatalf("%+v", e)
	}
	if e.Before == nil || *e.Before != 100 || e.After == nil || *e.After != 98.7 {
		t.Fatalf("before/after 不对：%+v", e)
	}
	if e.Skipped != "" {
		t.Fatalf("正常扣减不该带 skipped：%q", e.Skipped)
	}
}

// 免费模型（credit=0）不扣减，但**仍要发事件并标明原因** ——
// 否则 I2a 无法区分「免费所以没扣」与「该扣却没扣」。
func TestZeroCreditEmitsSkippedEvent(t *testing.T) {
	pool := newObserverPool(t)
	remaining := 100.0
	acc := CreateAccount(Account{Label: "one", BearerToken: "tok", Site: "domestic", QuotaRemaining: &remaining})
	if _, _, err := pool.Upsert(acc); err != nil {
		t.Fatal(err)
	}
	obs := &fakeObserver{}
	pool.SetObserver(obs)

	sel, err := pool.Select(SelectOptions{Site: "domestic", RequestID: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.MarkResult(sel, true, "", 0, 0); err != nil {
		t.Fatal(err)
	}
	if len(obs.quota) != 1 {
		t.Fatalf("应发 1 条事件（标明跳过原因），实际 %d", len(obs.quota))
	}
	e := obs.quota[0]
	if e.Op != QuotaOpDeduct || e.Skipped != DeductSkipNoCredit {
		t.Fatalf("应标 no_credit：%+v", e)
	}
	if e.Before == nil || e.After == nil || *e.Before != *e.After {
		t.Fatalf("跳过扣减时额度不该变：%+v", e)
	}
}

// 额度未知（nil）时同样跳过并标记，供 I2a 豁免。
func TestUnknownQuotaEmitsSkippedEvent(t *testing.T) {
	pool := newObserverPool(t)
	acc := CreateAccount(Account{Label: "one", BearerToken: "tok", Site: "domestic"})
	if _, _, err := pool.Upsert(acc); err != nil {
		t.Fatal(err)
	}
	obs := &fakeObserver{}
	pool.SetObserver(obs)

	sel, err := pool.Select(SelectOptions{Site: "domestic", RequestID: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.MarkResult(sel, true, "", 0, 2.0); err != nil {
		t.Fatal(err)
	}
	if len(obs.quota) != 1 || obs.quota[0].Skipped != DeductSkipQuotaUnknown {
		t.Fatalf("应标 quota_unknown：%+v", obs.quota)
	}
}

// 额度被清空时必须带理由 —— I1 靠它区分「有据清空」与「无声抹掉」。
func TestClearEmitsReason(t *testing.T) {
	pool := newObserverPool(t)

	healthy := 50.0
	acc := CreateAccount(Account{
		Label: "cooling", BearerToken: "tok", Site: "domestic",
		QuotaRemaining: &healthy,
		QuotaCheckedAt: time.Now().UnixMilli(),
		QuotaResetAt:   time.Now().Add(time.Hour).UnixMilli(),
		CooldownUntil:  time.Now().Add(time.Hour).UnixMilli(),
		CooldownSource: CooldownSourceUpstream,
	})
	if _, _, err := pool.Upsert(acc); err != nil {
		t.Fatal(err)
	}
	obs := &fakeObserver{}
	pool.SetObserver(obs)

	sel, err := pool.Select(SelectOptions{Site: "domestic", RequestID: "r2"})
	if err != nil {
		t.Fatal(err)
	}
	if !sel.BypassedCooldown {
		t.Fatalf("应走全冷却兜底：%+v", sel)
	}

	drained := 0.0
	if _, err := pool.ApplyQuotaProbe(sel.Account.ID, 0, &drained, false,
		time.Now().Add(time.Hour).UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := pool.MarkResult(sel, true, "", 0, 2.5); err != nil {
		t.Fatal(err)
	}

	var clear *QuotaEvent
	for i := range obs.quota {
		if obs.quota[i].Op == QuotaOpClear {
			clear = &obs.quota[i]
			break
		}
	}
	if clear == nil {
		t.Fatalf("应发 clear 事件，实际 %+v", obs.quota)
	}
	if clear.Before == nil || clear.After != nil {
		t.Fatalf("clear 的 before/after 不对：%+v", clear)
	}
	if clear.Reason != ClearReasonQuotaRecovered {
		t.Fatalf("理由应为 %q，实际 %q", ClearReasonQuotaRecovered, clear.Reason)
	}
}

// 对照：免费模型（credit=0）不得清空额度。
func TestFreeModelDoesNotClearQuota(t *testing.T) {
	pool := newObserverPool(t)
	healthy := 50.0
	acc := CreateAccount(Account{
		Label: "cooling", BearerToken: "tok", Site: "domestic",
		QuotaRemaining: &healthy,
		QuotaCheckedAt: time.Now().UnixMilli(),
		QuotaResetAt:   time.Now().Add(time.Hour).UnixMilli(),
		CooldownUntil:  time.Now().Add(time.Hour).UnixMilli(),
		CooldownSource: CooldownSourceUpstream,
	})
	if _, _, err := pool.Upsert(acc); err != nil {
		t.Fatal(err)
	}
	obs := &fakeObserver{}
	pool.SetObserver(obs)

	sel, err := pool.Select(SelectOptions{Site: "domestic", RequestID: "r5"})
	if err != nil {
		t.Fatal(err)
	}
	drained := 0.0
	if _, err := pool.ApplyQuotaProbe(sel.Account.ID, 0, &drained, false,
		time.Now().Add(time.Hour).UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := pool.MarkResult(sel, true, "", 0, 0); err != nil {
		t.Fatal(err)
	}
	for _, e := range obs.quota {
		if e.Op == QuotaOpClear {
			t.Fatalf("免费模型不得清空额度：%+v", e)
		}
	}
}

// 探针刷新要带刷新前的本地累计消耗，且 After 必须已经扣掉在途消耗。
func TestProbeEmitsRefreshWithPreviousSpent(t *testing.T) {
	pool := newObserverPool(t)
	remaining := 100.0
	acc := CreateAccount(Account{
		Label: "one", BearerToken: "tok", Site: "domestic",
		QuotaRemaining: &remaining, QuotaCheckedAt: time.Now().UnixMilli(),
	})
	if _, _, err := pool.Upsert(acc); err != nil {
		t.Fatal(err)
	}
	obs := &fakeObserver{}
	pool.SetObserver(obs)

	sel, err := pool.Select(SelectOptions{Site: "domestic", RequestID: "r3"})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.MarkResult(sel, true, "", 0, 4); err != nil {
		t.Fatal(err)
	}

	// baseline=0 且已有 4 的累计消耗 ⇒ in-flight 校正应扣掉 4。
	fresh := 96.0
	if _, err := pool.ApplyQuotaProbe(sel.Account.ID, 0, &fresh, false,
		time.Now().Add(time.Hour).UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}

	var refresh *QuotaEvent
	for i := range obs.quota {
		if obs.quota[i].Op == QuotaOpRefresh {
			refresh = &obs.quota[i]
			break
		}
	}
	if refresh == nil {
		t.Fatalf("应发 refresh 事件，实际 %+v", obs.quota)
	}
	if refresh.SpentSinceCheck != 4 {
		t.Fatalf("SpentSinceCheck 应为刷新前的累计值 4，实际 %v", refresh.SpentSinceCheck)
	}
	// After 必须是「余额 - 在途消耗」，这样 I2b 的 drift 才为 0。
	if refresh.After == nil || *refresh.After != 92 {
		t.Fatalf("After 应已扣除在途消耗 4（96-4=92），实际 %v", refresh.After)
	}
}

// 未安装观察者时不得 panic（生产默认路径）。
func TestNilObserverIsSafe(t *testing.T) {
	pool := newObserverPool(t)
	remaining := 10.0
	acc := CreateAccount(Account{Label: "one", BearerToken: "tok", Site: "domestic", QuotaRemaining: &remaining})
	if _, _, err := pool.Upsert(acc); err != nil {
		t.Fatal(err)
	}
	sel, err := pool.Select(SelectOptions{Site: "domestic", RequestID: "r4"})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.MarkResult(sel, true, "", 0, 1); err != nil {
		t.Fatal(err)
	}
	pool.SetObserver(nil)
	if err := pool.MarkResult(sel, true, "", 0, 1); err != nil {
		t.Fatal(err)
	}
}
