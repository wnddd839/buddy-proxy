package provider

import (
	"strings"
	"testing"
)

// 2026-10-17 复核 P0：Delivered 必须表示"真的下发给客户端"，
// 而不是"解析到了事件"。否则截断工具导致的空回合会被静默放行成 200 空响应。

const truncatedToolSSE = "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"run\",\"arguments\":\"{\\\"cmd\\\":\\\"ls\"}}]}}]}\n\n" +
	"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
	"data: [DONE]\n\n"

// 非流式路径（无任何回调）：解析到 tool_call_delta 也不算已交付。
func TestDeliveredFalseForNonStreamingTruncatedTool(t *testing.T) {
	c := &Client{}
	result, err := c.readSSE(strings.NewReader(truncatedToolSSE), ChatOptions{})
	if err != nil {
		t.Fatalf("readSSE: %v", err)
	}
	if len(result.Turn.ToolUses) != 0 {
		t.Fatalf("truncated tool must be dropped, got %+v", result.Turn.ToolUses)
	}
	if result.Turn.Delivered {
		t.Fatal("non-streaming path has no callbacks: Delivered must be false")
	}
	if !isEmptyCompletedTurn(result.Turn) {
		t.Fatal("empty turn with no delivery must stay replayable")
	}
}

// chat 流式：OnEvent 只转发 tool_use、忽略 tool_call_delta。
func TestDeliveredFalseForChatStreamingToolDelta(t *testing.T) {
	c := &Client{}
	opts := ChatOptions{
		OnEvent: func(event Event) {
			if event.Type != "tool_use" {
				return
			}
		},
		OnDelta: func(string) {},
	}
	result, err := c.readSSE(strings.NewReader(truncatedToolSSE), opts)
	if err != nil {
		t.Fatalf("readSSE: %v", err)
	}
	if result.Turn.Delivered {
		t.Fatal("chat streaming ignores tool_call_delta: Delivered must be false")
	}
	if !isEmptyCompletedTurn(result.Turn) {
		t.Fatal("empty turn must stay replayable when delta was never forwarded")
	}
}

// Responses 流式：真的转发 tool_call_delta（DeliverToolDeltas=true）。
func TestDeliveredTrueForResponsesStreamingToolDelta(t *testing.T) {
	c := &Client{}
	opts := ChatOptions{DeliverToolDeltas: true, OnEvent: func(Event) {}}
	result, err := c.readSSE(strings.NewReader(truncatedToolSSE), opts)
	if err != nil {
		t.Fatalf("readSSE: %v", err)
	}
	if !result.Turn.Delivered {
		t.Fatal("Responses streaming forwards tool_call_delta: Delivered must be true")
	}
	if isEmptyCompletedTurn(result.Turn) {
		t.Fatal("delivered content must not be judged empty")
	}
}

// 正文增量 + OnDelta：非空回调下才算已交付。
func TestDeliveredTrueForStreamedText(t *testing.T) {
	c := &Client{}
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"
	result, err := c.readSSE(strings.NewReader(body), ChatOptions{OnDelta: func(string) {}})
	if err != nil {
		t.Fatalf("readSSE: %v", err)
	}
	if !result.Turn.Delivered {
		t.Fatal("streamed text with OnDelta must count as delivered")
	}
}
