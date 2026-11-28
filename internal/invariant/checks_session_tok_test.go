package invariant

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
	"github.com/wnddd839/codebuddy-proxy/internal/tokfp"
)

// I10：同一会话内分词器比例突变。
//
// 为什么需要它（I9 覆盖不到）：
//
//	I9 用全局基线，需要同一 (模型,分桶) 攒够 3 个样本才判定。
//	若上游只对**某个会话**做路由（会话内切模型），全局基线会被
//	其他正常会话稀释，永远看不出。
//	I10 只需同一会话的前后两次 —— 长会话第一个样本就能建参照。
func TestI10CatchesSessionRatioJump(t *testing.T) {
	st := NewState(64, 64)

	// 第一次：建立该会话的参照值
	DefaultRegistry().Run(decision.Record{
		Kind: decision.KindRequest, At: 1, Ok: true, RequestID: "r1",
		SessionKey: "sess1", AccountID: "a1", Model: "deepseek-v4.1-flash",
		TokBucket: tokfp.BucketDigits, TokRatio: 0.4552,
	}, st)

	// 第二次：同会话突变 0.4552 → 0.6018（差 32%）
	got := DefaultRegistry().Run(decision.Record{
		Kind: decision.KindRequest, At: 2, Ok: true, RequestID: "r2",
		SessionKey: "sess1", AccountID: "a1", Model: "deepseek-v4.1-flash",
		TokBucket: tokfp.BucketDigits, TokRatio: 0.6018,
	}, st)

	var hit *decision.Finding
	for i := range got {
		if got[i].Invariant == "I10" {
			hit = &got[i]
		}
	}
	if hit == nil {
		t.Fatalf("同会话 32%% 突变应命中 I10：%+v", got)
	}
	if hit.Severity != SeverityViolation {
		t.Fatalf("I10 必须是 violation：%+v", hit)
	}
}

// 稳定比例不出 I10。
func TestI10SilentOnStableRatio(t *testing.T) {
	st := NewState(64, 64)
	for i := 0; i < 5; i++ {
		DefaultRegistry().Run(decision.Record{
			Kind: decision.KindRequest, At: int64(i), Ok: true,
			SessionKey: "s1", AccountID: "a1", Model: "m",
			TokBucket: tokfp.BucketCJK, TokRatio: 0.6371,
		}, st)
	}
}

// 不同会话之间不互相参照 —— 每个会话各自独立。
func TestI10IsolatesAcrossSessions(t *testing.T) {
	st := NewState(64, 64)
	DefaultRegistry().Run(decision.Record{
		Kind: decision.KindRequest, At: 1, Ok: true,
		SessionKey: "sA", AccountID: "a1", Model: "m",
		TokBucket: tokfp.BucketDigits, TokRatio: 0.4552,
	}, st)
	// 另一个会话用另一个比例：不该被判突变
	got := DefaultRegistry().Run(decision.Record{
		Kind: decision.KindRequest, At: 2, Ok: true,
		SessionKey: "sB", AccountID: "a1", Model: "m",
		TokBucket: tokfp.BucketDigits, TokRatio: 0.6018,
	}, st)
	for _, f := range got {
		if f.Invariant == "I10" {
			t.Fatalf("不同会话不该互相参照：%+v", f)
		}
	}
}

// 无会话键 / tiny 桶 / 未观测 ratio / 失败 / 取消 都不参与。
func TestI10Exemptions(t *testing.T) {
	st := NewState(64, 64)
	for _, rec := range []decision.Record{
		{Kind: decision.KindRequest, Ok: true, SessionKey: "", TokBucket: tokfp.BucketDigits, TokRatio: 0.9},
		{Kind: decision.KindRequest, Ok: true, SessionKey: "s", TokBucket: tokfp.BucketTiny, TokRatio: 0.9},
		{Kind: decision.KindRequest, Ok: true, SessionKey: "s", TokBucket: tokfp.BucketDigits, TokRatio: 0},
		{Kind: decision.KindRequest, Ok: false, SessionKey: "s", TokBucket: tokfp.BucketDigits, TokRatio: 0.9},
		{Kind: decision.KindRequest, Ok: true, Canceled: true, SessionKey: "s", TokBucket: tokfp.BucketDigits, TokRatio: 0.9},
	} {
		for _, f := range DefaultRegistry().Run(rec, st) {
			if f.Invariant == "I10" {
				t.Fatalf("豁免情形不该命中 I10：%+v（record=%+v）", f, rec)
			}
		}
	}
}

// 会话键必须包含模型与分桶：换模型或换桶就不是同一个参照系。
func TestI10KeyIncludesModelAndBucket(t *testing.T) {
	st := NewState(64, 64)
	DefaultRegistry().Run(decision.Record{
		Kind: decision.KindRequest, At: 1, Ok: true,
		SessionKey: "s", AccountID: "a1", Model: "m1",
		TokBucket: tokfp.BucketDigits, TokRatio: 0.4552,
	}, st)
	// 同会话但换模型：不该被判突变
	got := DefaultRegistry().Run(decision.Record{
		Kind: decision.KindRequest, At: 2, Ok: true,
		SessionKey: "s", AccountID: "a1", Model: "m2",
		TokBucket: tokfp.BucketDigits, TokRatio: 0.6018,
	}, st)
	for _, f := range got {
		if f.Invariant == "I10" {
			t.Fatalf("换模型不该判会话内突变：%+v", f)
		}
	}
}

// 样本容器有上限，不能无界增长。
func TestSessionTokIsBounded(t *testing.T) {
	st := NewState(8, 8) // sessionCap=8
	for i := 0; i < 20; i++ {
		st.ObserveSessionRatio("key"+string(rune('a'+i)), 0.5)
	}
	if n := st.SessionTokCount(); n > 8 {
		t.Fatalf("会话参照表应有上限 8，实际 %d", n)
	}
}
