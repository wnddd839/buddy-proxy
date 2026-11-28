package gateway

import (
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/decision"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

// reasoning 必须沿 RecordChat → Sink → jsonl 全链落盘。
//
// 只构造 decision.Record 直接喂 registry 是绕过链路的假测试：
// 它无法证明线上真的会把 reasoning_tokens 写进决策日志，而 I12 的
// 全部判据都依赖这两个字段 —— 字段没落盘，断言就永远看不见退化。
//
// 实测背景（这是 I12 存在的理由）：
//
//	11-24 18:33–20:11 的一个会话，systemFp 恒定 e7a34b5d、tier 恒定 max，
//	但父分支 167 条请求里 157 条 reasoning_tokens ≤ 1（用户直接感知成
//	「逐渐不思考、废话变多」）。既有 13 条断言一条都没抓到，因为
//	决策日志里根本没有 reasoning 字段。
func TestRecordChatWritesReasoningToDecisionLog(t *testing.T) {
	sink, store := newSinkForTest(t)
	svc := New(config.Config{Site: "domestic"}, nil)
	t.Cleanup(func() { _ = svc.Close() })
	svc.SetInvariants(sink)

	now := time.Now()
	svc.RecordChat(ChatRecordInput{
		Started:        now,
		ProxyRequestID: "req-reasoning",
		Model:          "deepseek-v4.1-flash",
		Ok:             true,
		Usage: provider.Usage{
			PromptTokens:     394,
			CompletionTokens: 1033,
			CompletionTokensDetails: &provider.CompletionTokensDetails{
				ReasoningTokens: 703,
			},
		},
		ReasoningRequested: true,
		ReasoningEffort:    "max",
		ReasoningTier:      "max",
	})

	sink.Stop()
	rows, err := store.ReadKind(now.Add(-time.Minute), now.Add(time.Minute), decision.KindRequest)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("应有 1 条 request 记录，实际 %d", len(rows))
	}
	rec := rows[0]
	if rec.ReasoningTokens != 703 {
		t.Fatalf("ReasoningTokens 应为 703，实际 %d —— I12 会因此永远看不到思考量", rec.ReasoningTokens)
	}
	if rec.ReasoningTier != "max" {
		t.Fatalf("ReasoningTier 应为 max，实际 %q —— I12 会因此把全部请求判成不可判", rec.ReasoningTier)
	}
	if rec.CompletionTokens != 1033 {
		t.Fatalf("CompletionTokens 应为 1033，实际 %d", rec.CompletionTokens)
	}
}
