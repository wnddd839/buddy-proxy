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
}

// 免费模型（credit=0）不扣减，因此也不该发 deduct 事件。
func TestZeroCreditEmitsNothing(t *testing.T) {
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
	if len(obs.quota) != 0 {
		t.Fatalf("credit=0 不该发事件：%+v", obs.quota)
	}
}

// 额度被清空时必须带理由 —— I1 靠它区分「有据清空」与「无声抹掉」。
//
// 真实路径（也是「免费模型是清空钥匙」那个后门的形态）：
// 选号时号处于 upstream 冷却、额度正常 ⇒ 走全冷却兜底（BypassedCooldown）；
// **请求进行中**并发探针把额度刷成 0 并打上 quota 冷却；
// 请求成功且真实扣费（credit>0）⇒ 旧快照被证明过期 ⇒ 清空并附理由。
func TestClearEmitsReason(t *testing.T) {
	pool := newObserverPool(t)

	healthy := 50.0
	acc := CreateAccount(Account{
		Label: "cooling", BearerToken: "tok", Site: "domestic",
		QuotaRemaining: &healthy,
		QuotaCheckedAt: time.Now().UnixMilli(),
		QuotaResetAt:   time.Now().Add(time.Hour).UnixMilli(),
		// 只处于 upstream 冷却：额度仍可用，所以选号会走全冷却兜底。
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

	// 请求进行中，探针把余额刷成 0 并打上 quota 冷却。
	drained := 0.0
	if _, err := pool.ApplyQuotaProbe(sel.Account.ID, 0, &drained, false,
		time.Now().Add(time.Hour).UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}

	// 付费成功（credit>0）才允许清空。
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
	if clear.Reason == "" {
		t.Fatal("clear 必须带 reason")
	}
}

// 对照：同样路径但免费模型（credit=0）⇒ **不得**清空。
// 这是 2026-11 那个后门的直接症状，必须有测试钉住。
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

// 探针刷新也要发事件，且带刷新前的本地累计消耗（I2b 靠它做漂移对账）。
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

	// 先记一笔真实消耗，让 QuotaSpentSinceCheck 非零。
	sel, err := pool.Select(SelectOptions{Site: "domestic", RequestID: "r3"})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.MarkResult(sel, true, "", 0, 4); err != nil {
		t.Fatal(err)
	}

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
