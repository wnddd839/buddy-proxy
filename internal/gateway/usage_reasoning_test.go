package gateway

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

// reasoning_tokens 必须落盘，否则无法区分「上游慢」与「预算被思考吃掉」。
//
// 实测背景（deepseek-v4.1-flash）：
//
//	high 档：reasoning 占 completion 77%（思考 948 字 / 正文 213 字）
//	max  档：reasoning 占 completion 96%（思考 6648 字 / 正文 199 字）
//
// 只看 completion_tokens 会把这两件事混成一件。
func TestRecordChatStoresReasoningTokens(t *testing.T) {
	svc := New(config.Config{Site: "domestic"}, nil)
	t.Cleanup(func() { _ = svc.Close() })

	svc.RecordChat(ChatRecordInput{
		Model:  "deepseek-v4.1-flash",
		Ok:     true,
		Stream: true,
		Usage: provider.Usage{
			PromptTokens:     100,
			CompletionTokens: 500,
			CompletionTokensDetails: &provider.CompletionTokensDetails{
				ReasoningTokens: 400,
			},
		},
		ReasoningRequested: true,
		ReasoningEffort:    "max",
	})

	reqs := svc.Journal.View("day", 10, 0).Requests
	if len(reqs) != 1 {
		t.Fatalf("应有 1 条用量记录，实际 %d", len(reqs))
	}
	e := reqs[0]
	if e.ReasoningTokens != 400 {
		t.Fatalf("ReasoningTokens 应为 400，实际 %d", e.ReasoningTokens)
	}
	if e.CompletionTokens != 500 {
		t.Fatalf("CompletionTokens 应为 500，实际 %d", e.CompletionTokens)
	}
	if e.ReasoningEffort != "max" {
		t.Fatalf("ReasoningEffort 应为 max，实际 %q", e.ReasoningEffort)
	}
}

// 没有 thinking 的请求也要能落盘，且 reasoning 字段留 0（omitempty 不写）。
func TestRecordChatWithoutReasoningTokens(t *testing.T) {
	svc := New(config.Config{Site: "domestic"}, nil)
	t.Cleanup(func() { _ = svc.Close() })

	svc.RecordChat(ChatRecordInput{
		Model: "hy4flash-free",
		Ok:    true,
		Usage: provider.Usage{PromptTokens: 10, CompletionTokens: 20},
	})

	reqs := svc.Journal.View("day", 10, 0).Requests
	if len(reqs) != 1 {
		t.Fatalf("应有 1 条用量记录，实际 %d", len(reqs))
	}
	if reqs[0].ReasoningTokens != 0 {
		t.Fatalf("无 thinking 时应为 0，实际 %d", reqs[0].ReasoningTokens)
	}
}
