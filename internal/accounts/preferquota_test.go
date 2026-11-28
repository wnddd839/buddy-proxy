package accounts

import (
	"testing"
	"time"
)

// PreferQuota 快路径也必须把 RequestID 带到 Selection 上。
//
// 上线首分钟的真实缺陷：这条快路径返回的 Selection 没有 RequestID，
// 于是 deduct 事件的 requestID 为空，I2a 立刻报「上游扣费 4.05，
// 但没有对应的扣减记录」。
func TestPreferQuotaPathCarriesRequestID(t *testing.T) {
	pool := newObserverPool(t)
	remaining := 100.0
	acc := CreateAccount(Account{
		Label: "rich", BearerToken: "tok", Site: "global",
		QuotaRemaining: &remaining,
		QuotaCheckedAt: time.Now().UnixMilli(),
		QuotaResetAt:   time.Now().Add(time.Hour).UnixMilli(),
	})
	if _, _, err := pool.Upsert(acc); err != nil {
		t.Fatal(err)
	}
	obs := &fakeObserver{}
	pool.SetObserver(obs)

	sel, err := pool.Select(SelectOptions{
		Site: "global", Model: "m", PreferQuota: true, RequestID: "req-pq",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sel.RequestID != "req-pq" {
		t.Fatalf("PreferQuota 快路径丢了 RequestID：%+v", sel)
	}
	if len(obs.selection) != 1 {
		t.Fatalf("快路径也要发选号事件，实际 %d", len(obs.selection))
	}
	if obs.selection[0].RequestID != "req-pq" {
		t.Fatalf("事件里的 RequestID 也不对：%+v", obs.selection[0])
	}

	// 扣减事件必须能带上同一个 requestID（I2a 靠它配对）。
	if err := pool.MarkResult(sel, true, "", 0, 1.5); err != nil {
		t.Fatal(err)
	}
	var deduct *QuotaEvent
	for i := range obs.quota {
		if obs.quota[i].Op == QuotaOpDeduct {
			deduct = &obs.quota[i]
			break
		}
	}
	if deduct == nil {
		t.Fatalf("应发 deduct 事件：%+v", obs.quota)
	}
	if deduct.RequestID != "req-pq" {
		t.Fatalf("deduct 事件的 RequestID 应为 req-pq，实际 %q", deduct.RequestID)
	}
}
