package gateway

import (
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/decision"
	"github.com/wnddd839/codebuddy-proxy/internal/invariant"
)

// InvariantObserver 把 accounts 的观察者事件翻译成 decision.Record 并喂给 Sink。
//
// 放在 gateway 层而不是 accounts 层的原因：accounts 不该依赖 invariant
// （那会让纯账号池包带上观测体系的全部传递依赖），
// 而 gateway 本来就同时知道两者。
//
// 注意：这些回调发生在 Pool 持有锁期间（见 accounts.Observer 的契约），
// 所以这里只做「构造 Record + 调 Sink.Submit」，绝不可回调 Pool。
type InvariantObserver struct {
	Sink *invariant.Sink
}

// ObserveQuota 处理额度变更。
func (o *InvariantObserver) ObserveQuota(e accounts.QuotaEvent) {
	if o == nil || o.Sink == nil {
		return
	}
	rec := decision.Record{
		Kind:            decision.KindQuota,
		At:              time.Now().UnixMilli(),
		AccountID:       e.AccountID,
		RequestID:       e.RequestID,
		Op:              e.Op,
		Before:          e.Before,
		After:           e.After,
		Credit:          creditPtr(e.Credit),
		SpentSinceCheck: e.SpentSinceCheck,
		CheckedAtBefore: e.CheckedAtBefore,
		CheckedAtAfter:  e.CheckedAtAfter,
		Reason:          e.Reason,
	}
	// I2a 靠这条把「本地扣了多少」与稍后到达的 request 记录对上。
	if e.Op == accounts.QuotaOpDeduct && e.RequestID != "" {
		o.Sink.RememberDeduct(e.RequestID, e.Credit)
	}
	o.Sink.Submit(rec)
}

// ObserveSelection 处理选号结果。
func (o *InvariantObserver) ObserveSelection(e accounts.SelectionEvent) {
	if o == nil || o.Sink == nil {
		return
	}
	o.Sink.Submit(decision.Record{
		Kind:       decision.KindSelection,
		At:         time.Now().UnixMilli(),
		RequestID:  e.RequestID,
		Site:       e.Site,
		Model:      e.Model,
		Candidates: e.Candidates,
		Chosen:     e.Chosen,
		Skipped:    e.Skipped,
	})
}

// creditPtr：0 表示「免费/无扣费」，与「上游没回字段」是两件事，
// 所以这里保留指针语义（0 → 指针，未传 → nil 由调用方决定）。
func creditPtr(v float64) *float64 {
	if v == 0 {
		return nil
	}
	return &v
}
