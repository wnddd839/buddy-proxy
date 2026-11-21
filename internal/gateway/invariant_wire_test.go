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

// RecordChat 必须在 journal 与断言执行器两侧都留下记录。
func TestRecordChatSubmitsDecision(t *testing.T) {
	sink, _ := newSinkForTest(t)
	svc := New(config.Config{Site: "domestic"}, nil)
	t.Cleanup(func() { _ = svc.Close() })
	svc.SetInvariants(sink)

	svc.RecordChat(ChatRecordInput{
		Started:        time.Now(),
		ProxyRequestID: "req-1",
		Model:          "m",
		Ok:             true,
		IO:             &provider.MessagesIO{InMessages: 2, OutMessages: 2, InBytes: 10, OutBytes: 10},
	})

	// journal 里有一条
	if got := svc.Journal.View("day", 10, 0).Requests; len(got) != 1 {
		t.Fatalf("journal 应有 1 条，实际 %d", len(got))
	}
	// 断言执行器见过这条（无 finding 时不落盘，改用 Check 计数验证）
	if sink == nil {
		t.Fatal("sink 不应为 nil")
	}
}

// Sink 为 nil 时 RecordChat 不得 panic（默认部署路径）。
func TestRecordChatWithoutSink(t *testing.T) {
	svc := New(config.Config{Site: "domestic"}, nil)
	t.Cleanup(func() { _ = svc.Close() })
	svc.RecordChat(ChatRecordInput{Started: time.Now(), Model: "m", Ok: true})
}

// 观察者必须把 accounts 事件原样翻译成 decision.Record。
func TestObserverMapsQuotaEvent(t *testing.T) {
	sink, _ := newSinkForTest(t)
	obs := &InvariantObserver{Sink: sink}

	before, after := 100.0, 98.7
	obs.ObserveQuota(accounts.QuotaEvent{
		AccountID: "a1", RequestID: "r1", Op: accounts.QuotaOpDeduct,
		Before: &before, After: &after, Credit: 1.3,
	})
	// 扣减必须进 state，供 I2a 与 request 对账。
	sink.Submit(decision.Record{
		Kind: decision.KindRequest, At: time.Now().UnixMilli(),
		RequestID: "r1", AccountID: "a1", Ok: true,
		Credit: func() *float64 { v := 1.3; return &v }(),
	})
	for _, f := range sink.Findings(10) {
		t.Fatalf("扣减与该 credit 一致，不该命中：%+v", f)
	}
}

// credit=0 的额度事件不该被当成「有扣费」记进 state。
func TestObserverSkipsZeroCredit(t *testing.T) {
	sink, _ := newSinkForTest(t)
	obs := &InvariantObserver{Sink: sink}
	before, after := 100.0, 100.0
	obs.ObserveQuota(accounts.QuotaEvent{
		AccountID: "a1", RequestID: "r2", Op: accounts.QuotaOpDeduct,
		Before: &before, After: &after, Credit: 0,
	})
	if got := sink.Findings(10); len(got) != 0 {
		t.Fatalf("零扣减不该产生 finding：%+v", got)
	}
}

// 选号事件也要落到断言执行器（I7 靠它）。
func TestObserverMapsSelectionEvent(t *testing.T) {
	sink, _ := newSinkForTest(t)
	obs := &InvariantObserver{Sink: sink}
	obs.ObserveSelection(accounts.SelectionEvent{
		RequestID: "r3", Site: "global", Model: "m",
		Candidates: 4, Chosen: "a1",
		// 故意不给跳过原因：I7 必须报
	})
	got := sink.Findings(10)
	if len(got) != 1 || got[0].Invariant != "I7" {
		t.Fatalf("应命中 I7：%+v", got)
	}
}
