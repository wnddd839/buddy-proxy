package provider

import (
	"strings"
	"testing"
)

// 回归：上游以 FIN 关闭但没发终止帧（finish_reason / [DONE]）时，绝不能把半截
// 正文当正常完成——旧行为会让 FinishReasonFromTurn 默认补 stop，客户端看到的是
// 「会话说到一半直接没了」。
func TestReadSSETruncatedAfterDelivery(t *testing.T) {
	body := strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"半句话\"}}]}\n\n")
	var got strings.Builder
	result, err := (&Client{}).readSSE(body, ChatOptions{OnDelta: func(s string) { got.WriteString(s) }})
	if err == nil {
		t.Fatal("expected truncation error, got nil")
	}
	if !strings.Contains(err.Error(), "truncated after delivering content") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.String() != "半句话" {
		t.Fatalf("content must still reach the client once, got %q", got.String())
	}
	if !result.Turn.Delivered {
		t.Fatal("Delivered must be true so the service layer does not replay and duplicate")
	}
}

// 首内容前就断流：客户端什么都没看到，应报可重放的错误（服务层换号重试）。
func TestReadSSETruncatedBeforeDelivery(t *testing.T) {
	body := strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"半句话\"}}]}\n\n")
	_, err := (&Client{}).readSSE(body, ChatOptions{})
	if err == nil {
		t.Fatal("expected truncation error, got nil")
	}
	if !strings.Contains(err.Error(), "without terminal event before first content") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 正常收尾（finish_reason + [DONE]）不得被误判成截断。
func TestReadSSECompleteTurnUnaffected(t *testing.T) {
	for name, body := range map[string]string{
		"finish_then_done": "data: {\"choices\":[{\"delta\":{\"content\":\"完整回答\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n",
		"done_only": "data: {\"choices\":[{\"delta\":{\"content\":\"完整回答\"}}]}\n\n" +
			"data: [DONE]\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			var got strings.Builder
			result, err := (&Client{}).readSSE(strings.NewReader(body),
				ChatOptions{OnDelta: func(s string) { got.WriteString(s) }})
			if err != nil {
				t.Fatalf("complete turn must not error: %v", err)
			}
			if got.String() != "完整回答" {
				t.Fatalf("content mismatch: %q", got.String())
			}
			if result.Turn.StopReason == "error" {
				t.Fatalf("unexpected stop reason: %s", result.Turn.StopReason)
			}
		})
	}
}

// 完全没有内容的空流仍走「空回合」判定，不应被新逻辑拦成截断错误。
func TestReadSSEEmptyStreamStillEmptyTurn(t *testing.T) {
	_, err := (&Client{}).readSSE(strings.NewReader(": keep-alive\n\n"),
		ChatOptions{OnDelta: func(string) {}})
	if err != nil {
		t.Fatalf("empty stream should not error here: %v", err)
	}
}
