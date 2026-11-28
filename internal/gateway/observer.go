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
// 锁序警告：这些回调发生在 Pool 持有 p.mu 期间（见 accounts.Observer 的契约）。
// 因此这里只允许做「构造 Record + 调 Sink.Submit」，后者只碰内存与
// 有界 channel（锁序 Pool.mu → State.mu）。**绝不可回调 Pool 的方法**，
// 反向获取锁必死锁。
type InvariantObserver struct {
	Sink *invariant.Sink
}

// ObserveQuota 处理额度变更。
func (o *InvariantObserver) ObserveQuota(e accounts.QuotaEvent) {
	if o == nil || o.Sink == nil {
		return
	}
	// 实际扣减量用 Before-After 计算，而不是 e.Credit。
	// 两者通常相等，但 Before/After 是「额度真的动了多少」的直接证据；
	// 一旦将来扣减逻辑引入折扣/多扣，I2a 抓的是真实差额而不是声明值。
	amount := 0.0
	if e.Before != nil && e.After != nil {
		amount = *e.Before - *e.After
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
		SkipReason:      e.Skipped,
	}
	// 扣减事件：记下「这个请求扣了多少 / 为什么没扣」，供 I2a 与
	// 稍后到达的 request 记录对账。
	if e.RequestID != "" {
		switch e.Op {
		case accounts.QuotaOpDeduct:
			o.Sink.RememberDeduct(e.RequestID, amount, e.Skipped)
		case accounts.QuotaOpClear:
			// 清空快照路径（全冷却兜底 + 原本额度阻塞 + credit>0）：
			// 它的语义是「旧快照过期，重建」，**不扣减**。
			// 必须显式标成「设计内跳过」，否则 I2a 会报「漏记」。
			o.Sink.RememberDeduct(e.RequestID, 0, accounts.DeductSkipQuotaCleared)
		}
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
		Examined:   e.Examined,
		SelSource:  e.Source,
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
