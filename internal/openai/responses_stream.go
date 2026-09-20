package openai

import (
	"sync/atomic"

	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

// ---------------------------------------------------------------------------
// Responses API 流式：SSE 事件状态机
// ---------------------------------------------------------------------------
//
// Codex CLI 与官方 openai SDK 按命名事件驱动（event: response.output_text.delta），
// 且依赖 output_item 生命周期帧定位 item。这里把内部 Event 回调转成
// OpenAI 规范的事件序列。sequence_number 单调递增，严格客户端用它排序。

// ResponseStreamEvent 是一个 SSE 帧；Type 决定事件名。
// OutputIndex / ContentIndex 用指针：0 也必须出现在 JSON 里（与 chat
// tool_calls[].index 同一条教训，#16），created/completed 不设则省略。
type ResponseStreamEvent struct {
	Type         string `json:"type"`
	ResponseID   string `json:"response_id,omitempty"`
	OutputIndex  *int   `json:"output_index,omitempty"`
	ItemID       string `json:"item_id,omitempty"`
	ContentIndex *int   `json:"content_index,omitempty"`
	Delta        string `json:"delta,omitempty"`
	Text         string `json:"text,omitempty"`
	// Arguments 仅用于 response.function_call_arguments.done：
	// 官方规范该事件携带完整 arguments 字段（与 output_text.done 的 text 对应）。
	Arguments      string                  `json:"arguments,omitempty"`
	Item           *ResponsesOutputItem    `json:"item,omitempty"`
	Part           *ResponsesOutputContent `json:"part,omitempty"`
	Response       *ResponseObject         `json:"response,omitempty"`
	SequenceNumber int64                   `json:"sequence_number"`
}

// ResponseStreamBuilder 累积一个响应周期内的事件状态。
type ResponseStreamBuilder struct {
	ResponseID string
	Model      string
	Req        *ResponsesRequest

	seq atomic.Int64

	outputIndex   int
	messageOpen   bool
	messageID     string
	reasoningOpen bool
	reasoningID   string
	toolOpen      map[int]bool

	textBuf     []byte
	thinkingBuf []byte
	toolArgs    map[int][]byte
	toolCallIDs map[int]string
	toolNames   map[int]string
	// toolItemIDs 记录每个 function_call item 的 id：added/delta/done 必须一致，
	// 严格客户端按 item_id 关联事件。
	toolItemIDs map[int]string
}

func NewResponseStreamBuilder(responseID, model string, req *ResponsesRequest) *ResponseStreamBuilder {
	return &ResponseStreamBuilder{
		ResponseID:  responseID,
		Model:       model,
		Req:         req,
		outputIndex: -1,
		toolOpen:    map[int]bool{},
		toolArgs:    map[int][]byte{},
		toolCallIDs: map[int]string{},
		toolNames:   map[int]string{},
		toolItemIDs: map[int]string{},
	}
}

func (b *ResponseStreamBuilder) nextSeq() int64 { return b.seq.Add(1) }

// MessageOpen 报告 message item 是否处于打开状态。
func (b *ResponseStreamBuilder) MessageOpen() bool { return b.messageOpen }

// ReasoningOpen 报告 reasoning item 是否处于打开状态。
func (b *ResponseStreamBuilder) ReasoningOpen() bool { return b.reasoningOpen }

// CreatedEvents 返回 response.created + response.in_progress。
func (b *ResponseStreamBuilder) CreatedEvents() []ResponseStreamEvent {
	resp := NewResponseObject(b.ResponseID, b.Model, b.Req)
	return []ResponseStreamEvent{
		{Type: "response.created", Response: &resp, SequenceNumber: b.nextSeq()},
		{Type: "response.in_progress", Response: &resp, SequenceNumber: b.nextSeq()},
	}
}

// ReasoningStart 打开 reasoning item。
func (b *ResponseStreamBuilder) ReasoningStart() []ResponseStreamEvent {
	if b.reasoningOpen {
		return nil
	}
	b.outputIndex++
	b.reasoningOpen = true
	b.reasoningID = NewItemID("rs")
	item := ResponsesOutputItem{ID: b.reasoningID, Type: "reasoning", Status: "in_progress"}
	return []ResponseStreamEvent{{
		Type:           "response.output_item.added",
		ResponseID:     b.ResponseID,
		OutputIndex:    intp(b.outputIndex),
		Item:           &item,
		SequenceNumber: b.nextSeq(),
	}}
}

// ReasoningDelta 推送推理文本增量。
func (b *ResponseStreamBuilder) ReasoningDelta(delta string) []ResponseStreamEvent {
	if !b.reasoningOpen {
		return nil
	}
	b.thinkingBuf = append(b.thinkingBuf, delta...)
	return []ResponseStreamEvent{{
		Type:           "response.reasoning_summary_text.delta",
		ResponseID:     b.ResponseID,
		ItemID:         b.reasoningID,
		OutputIndex:    intp(b.outputIndex),
		Delta:          delta,
		SequenceNumber: b.nextSeq(),
	}}
}

// ReasoningDone 关闭 reasoning item。
func (b *ResponseStreamBuilder) ReasoningDone() []ResponseStreamEvent {
	if !b.reasoningOpen {
		return nil
	}
	b.reasoningOpen = false
	item := ResponsesOutputItem{
		ID:      b.reasoningID,
		Type:    "reasoning",
		Status:  "completed",
		Summary: reasoningSummary(string(b.thinkingBuf)),
	}
	return []ResponseStreamEvent{{
		Type:           "response.output_item.done",
		ResponseID:     b.ResponseID,
		OutputIndex:    intp(b.outputIndex),
		Item:           &item,
		SequenceNumber: b.nextSeq(),
	}}
}

// MessageStart 打开 message item + content_part。
func (b *ResponseStreamBuilder) MessageStart() []ResponseStreamEvent {
	if b.messageOpen {
		return nil
	}
	var events []ResponseStreamEvent
	if b.reasoningOpen {
		events = append(events, b.ReasoningDone()...)
	}
	b.outputIndex++
	b.messageOpen = true
	b.messageID = NewItemID("msg")
	item := ResponsesOutputItem{
		ID:      b.messageID,
		Type:    "message",
		Status:  "in_progress",
		Role:    "assistant",
		Content: []ResponsesOutputContent{},
	}
	events = append(events, ResponseStreamEvent{
		Type:           "response.output_item.added",
		ResponseID:     b.ResponseID,
		OutputIndex:    intp(b.outputIndex),
		Item:           &item,
		SequenceNumber: b.nextSeq(),
	})
	part := ResponsesOutputContent{Type: "output_text", Text: "", Annotations: []any{}}
	events = append(events, ResponseStreamEvent{
		Type:           "response.content_part.added",
		ResponseID:     b.ResponseID,
		ItemID:         b.messageID,
		OutputIndex:    intp(b.outputIndex),
		ContentIndex:   intp(0),
		Part:           &part,
		SequenceNumber: b.nextSeq(),
	})
	return events
}

// MessageDelta 推送文本增量。
func (b *ResponseStreamBuilder) MessageDelta(delta string) []ResponseStreamEvent {
	if !b.messageOpen {
		return nil
	}
	b.textBuf = append(b.textBuf, delta...)
	return []ResponseStreamEvent{{
		Type:           "response.output_text.delta",
		ResponseID:     b.ResponseID,
		ItemID:         b.messageID,
		OutputIndex:    intp(b.outputIndex),
		ContentIndex:   intp(0),
		Delta:          delta,
		SequenceNumber: b.nextSeq(),
	}}
}

// MessageDone 关闭 message item。
func (b *ResponseStreamBuilder) MessageDone() []ResponseStreamEvent {
	if !b.messageOpen {
		return nil
	}
	b.messageOpen = false
	text := string(b.textBuf)
	events := []ResponseStreamEvent{{
		Type:           "response.output_text.done",
		ResponseID:     b.ResponseID,
		ItemID:         b.messageID,
		OutputIndex:    intp(b.outputIndex),
		ContentIndex:   intp(0),
		Text:           text,
		SequenceNumber: b.nextSeq(),
	}}
	part := ResponsesOutputContent{Type: "output_text", Text: text, Annotations: []any{}}
	events = append(events, ResponseStreamEvent{
		Type:           "response.content_part.done",
		ResponseID:     b.ResponseID,
		ItemID:         b.messageID,
		OutputIndex:    intp(b.outputIndex),
		ContentIndex:   intp(0),
		Part:           &part,
		SequenceNumber: b.nextSeq(),
	})
	item := ResponsesOutputItem{
		ID:      b.messageID,
		Type:    "message",
		Status:  "completed",
		Role:    "assistant",
		Content: []ResponsesOutputContent{part},
	}
	events = append(events, ResponseStreamEvent{
		Type:           "response.output_item.done",
		ResponseID:     b.ResponseID,
		OutputIndex:    intp(b.outputIndex),
		Item:           &item,
		SequenceNumber: b.nextSeq(),
	})
	return events
}

// ToolOpen 报告 function_call item 是否处于打开状态。
func (b *ResponseStreamBuilder) ToolOpen() bool { return b.toolOpen[b.outputIndex] }

// ToolCallStart 打开 function_call item（来自上游完整 tool_use 事件）。
// 自动关闭尚未完结的 tool / message / reasoning item，保证 output_item 生命周期合法。
func (b *ResponseStreamBuilder) ToolCallStart(callID, name string) []ResponseStreamEvent {
	var events []ResponseStreamEvent
	if b.ToolOpen() {
		events = append(events, b.ToolCallDone()...)
	}
	if b.messageOpen {
		events = append(events, b.MessageDone()...)
	}
	if b.reasoningOpen {
		events = append(events, b.ReasoningDone()...)
	}
	return append(events, b.toolCallStart(callID, name)...)
}

func (b *ResponseStreamBuilder) toolCallStart(callID, name string) []ResponseStreamEvent {
	b.outputIndex++
	b.toolOpen[b.outputIndex] = true
	if callID == "" {
		callID = NewItemID("call")
	}
	itemID := NewItemID("fc")
	b.toolCallIDs[b.outputIndex] = callID
	b.toolNames[b.outputIndex] = name
	b.toolItemIDs[b.outputIndex] = itemID
	item := ResponsesOutputItem{
		ID:        itemID,
		Type:      "function_call",
		Status:    "in_progress",
		CallID:    callID,
		Name:      name,
		Arguments: "",
	}
	return []ResponseStreamEvent{{
		Type:           "response.output_item.added",
		ResponseID:     b.ResponseID,
		OutputIndex:    intp(b.outputIndex),
		Item:           &item,
		SequenceNumber: b.nextSeq(),
	}}
}

// ToolCallDelta 推送 arguments 增量。
func (b *ResponseStreamBuilder) ToolCallDelta(argsDelta string) []ResponseStreamEvent {
	idx := b.outputIndex
	if !b.toolOpen[idx] {
		return nil
	}
	b.toolArgs[idx] = append(b.toolArgs[idx], argsDelta...)
	return []ResponseStreamEvent{{
		Type:           "response.function_call_arguments.delta",
		ResponseID:     b.ResponseID,
		ItemID:         b.toolItemIDs[idx],
		OutputIndex:    intp(idx),
		Delta:          argsDelta,
		SequenceNumber: b.nextSeq(),
	}}
}

// ToolCallDone 关闭 function_call item。
func (b *ResponseStreamBuilder) ToolCallDone() []ResponseStreamEvent {
	idx := b.outputIndex
	if !b.toolOpen[idx] {
		return nil
	}
	b.toolOpen[idx] = false
	args := string(b.toolArgs[idx])
	itemID := b.toolItemIDs[idx]
	item := ResponsesOutputItem{
		ID:        itemID,
		Type:      "function_call",
		Status:    "completed",
		CallID:    b.toolCallIDs[idx],
		Name:      b.toolNames[idx],
		Arguments: args,
	}
	return []ResponseStreamEvent{{
		Type:           "response.function_call_arguments.done",
		ResponseID:     b.ResponseID,
		ItemID:         itemID,
		OutputIndex:    intp(idx),
		Arguments:      args,
		SequenceNumber: b.nextSeq(),
	}, {
		Type:           "response.output_item.done",
		ResponseID:     b.ResponseID,
		OutputIndex:    intp(idx),
		Item:           &item,
		SequenceNumber: b.nextSeq(),
	}}
}

// Completed 关闭所有未完结 item 并返回 response.completed。
func (b *ResponseStreamBuilder) Completed(turn provider.Turn) []ResponseStreamEvent {
	var events []ResponseStreamEvent
	if b.reasoningOpen {
		events = append(events, b.ReasoningDone()...)
	}
	if b.messageOpen {
		events = append(events, b.MessageDone()...)
	}
	openUntil := b.outputIndex
	for idx := 0; idx <= openUntil; idx++ {
		if b.toolOpen[idx] {
			b.outputIndex = idx
			events = append(events, b.ToolCallDone()...)
		}
	}
	resp := ResponseFromTurn(turn, b.ResponseID, b.Model, b.Req)
	events = append(events, ResponseStreamEvent{
		Type:           "response.completed",
		Response:       &resp,
		SequenceNumber: b.nextSeq(),
	})
	return events
}

// Failed 生成 response.failed。
func (b *ResponseStreamBuilder) Failed(code, message string) ResponseStreamEvent {
	resp := FailedResponseObject(b.ResponseID, b.Model, b.Req, code, message)
	return ResponseStreamEvent{
		Type:           "response.failed",
		Response:       &resp,
		SequenceNumber: b.nextSeq(),
	}
}

func intp(n int) *int { return &n }
