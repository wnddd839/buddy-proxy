package gateway

import (
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/decision"
	"github.com/wnddd839/codebuddy-proxy/internal/invariant"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

func newSinkForTest(t *testing.T) (*invariant.Sink, *decision.Store) {
	t.Helper()
	store, err := decision.OpenStore(t.TempDir(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sink := invariant.NewSink(store, invariant.DefaultRegistry(), invariant.NewState(64, 64), 50, 128)
	sink.Start()
	t.Cleanup(sink.Stop)
	return sink, store
}

// RecordChat 必须同时写 journal 与断言执行器，且后者真的收到了这条记录。
func TestRecordChatSubmitsDecision(t *testing.T) {
	sink, store := newSinkForTest(t)
	svc := New(config.Config{Site: "domestic"}, nil)
	t.Cleanup(func() { _ = svc.Close() })
	svc.SetInvariants(sink)

	now := time.Now()
	svc.RecordChat(ChatRecordInput{
		Started:        now,
		ProxyRequestID: "req-1",
		Model:          "m",
		Ok:             true,
		IO:             &provider.MessagesIO{InMessages: 2, OutMessages: 2, InBytes: 10, OutBytes: 10},
	})

	if got := svc.Journal.View("day", 10, 0).Requests; len(got) != 1 {
		t.Fatalf("journal 应有 1 条，实际 %d", len(got))
	}

	// 断言执行器必须把这条记录落盘（否则离线回放没有输入）。
	sink.Stop()
	rows, err := store.ReadKind(now.Add(-time.Minute), now.Add(time.Minute), decision.KindRequest)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].RequestID != "req-1" {
		t.Fatalf("断言执行器应收到 request 决策，实际 %+v", rows)
	}
}

// Sink 为 nil 时 RecordChat 不得 panic（默认部署路径）。
func TestRecordChatWithoutSink(t *testing.T) {
	svc := New(config.Config{Site: "domestic"}, nil)
	t.Cleanup(func() { _ = svc.Close() })
	svc.RecordChat(ChatRecordInput{Started: time.Now(), Model: "m", Ok: true})
}

// 观察者必须把扣减额以「实际变化量」记进 state，供 I2a 对账。
func TestObserverMapsQuotaEvent(t *testing.T) {
	sink, _ := newSinkForTest(t)
	obs := &InvariantObserver{Sink: sink}

	before, after := 100.0, 98.7
	obs.ObserveQuota(accounts.QuotaEvent{
		AccountID: "a1", RequestID: "r1", Op: accounts.QuotaOpDeduct,
		Before: &before, After: &after, Credit: 1.3,
	})
	credit := 1.3
	sink.Submit(decision.Record{
		Kind: decision.KindRequest, At: time.Now().UnixMilli(),
		RequestID: "r1", AccountID: "a1", Ok: true, Credit: &credit,
	})
	for _, f := range sink.Findings(10) {
		t.Fatalf("扣减与该 credit 一致，不该命中：%+v", f)
	}
}

// 跳过扣减的事件必须带上原因，否则 I2a 会把它当成「漏记」。
func TestObserverCarriesSkipReason(t *testing.T) {
	sink, _ := newSinkForTest(t)
	obs := &InvariantObserver{Sink: sink}

	same := 100.0
	obs.ObserveQuota(accounts.QuotaEvent{
		AccountID: "a1", RequestID: "r2", Op: accounts.QuotaOpDeduct,
		Before: &same, After: &same, Credit: 0,
		Skipped: accounts.DeductSkipNoCredit,
	})
	if got := sink.Findings(10); len(got) != 0 {
		t.Fatalf("跳过扣减不该产生 finding：%+v", got)
	}
	// 该请求随后成功、上游 credit=0 ⇒ I2a 应放过。
	zero := 0.0
	sink.Submit(decision.Record{
		Kind: decision.KindRequest, At: time.Now().UnixMilli(),
		RequestID: "r2", AccountID: "a1", Ok: true, Credit: &zero,
	})
	if got := sink.Findings(10); len(got) != 0 {
		t.Fatalf("免费模型不该命中 I2a：%+v", got)
	}
}

// 选号事件也要落到断言执行器（I7 靠它）。
// 检查了 3 个号、选中 1 个、却没有任何跳过原因 ⇒ 必须报。
func TestObserverMapsSelectionEvent(t *testing.T) {
	sink, _ := newSinkForTest(t)
	obs := &InvariantObserver{Sink: sink}
	obs.ObserveSelection(accounts.SelectionEvent{
		RequestID: "r3", Site: "global", Model: "m",
		Candidates: 10, Examined: 3, Chosen: "a1",
	})
	got := sink.Findings(10)
	if len(got) != 1 || got[0].Invariant != "I7" {
		t.Fatalf("应命中 I7：%+v", got)
	}
}

// 健康池的首选（只检查 1 个、无跳过）不得报 —— 评审 C1 的场景。
func TestObserverHealthyFirstPickIsSilent(t *testing.T) {
	sink, _ := newSinkForTest(t)
	obs := &InvariantObserver{Sink: sink}
	obs.ObserveSelection(accounts.SelectionEvent{
		RequestID: "r4", Site: "global", Model: "m",
		Candidates: 243, Examined: 1, Chosen: "a1",
	})
	if got := sink.Findings(10); len(got) != 0 {
		t.Fatalf("健康池首选不该报：%+v", got)
	}
}
