package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

func eventTypes(events []ResponseStreamEvent) []string {
	types := make([]string, 0, len(events))
	for _, e := range events {
		types = append(types, e.Type)
	}
	return types
}

// assertContainsInOrder 校验 want 中每个元素按顺序出现在 got 中（允许中间有额外帧）。
func assertContainsInOrder(t *testing.T, got, want []string) {
	t.Helper()
	gi := 0
	for _, w := range want {
		found := false
		for gi < len(got) {
			if got[gi] == w {
				found = true
				gi++
				break
			}
			gi++
		}
		if !found {
			t.Fatalf("event %q not found in order; got %v", w, got)
		}
	}
}

func TestStreamBuilder_TextOnly(t *testing.T) {
	req := &ResponsesRequest{Model: "auto", Input: "hi"}
	b := NewResponseStreamBuilder("resp_t1", "auto", req)

	var all []ResponseStreamEvent
	all = append(all, b.CreatedEvents()...)
	all = append(all, b.MessageStart()...)
	all = append(all, b.MessageDelta("Hello")...)
	all = append(all, b.MessageDelta(", world")...)
	all = append(all, b.Completed(provider.Turn{Text: "Hello, world"})...)

	assertContainsInOrder(t, eventTypes(all), []string{
		"response.created", "response.in_progress",
		"response.output_item.added", "response.content_part.added",
		"response.output_text.delta", "response.output_text.delta",
		"response.output_text.done", "response.content_part.done",
		"response.output_item.done", "response.completed",
	})

	for i := 1; i < len(all); i++ {
		if all[i].SequenceNumber <= all[i-1].SequenceNumber {
			t.Fatalf("sequence_number not monotonic at %d: %d <= %d", i, all[i].SequenceNumber, all[i-1].SequenceNumber)
		}
	}
	// 第一条 output 事件的 output_index 必须是 0 且出现在 JSON 里（不能被 omitempty 吃掉）。
	var firstAdded []byte
	for _, e := range all {
		if e.Type == "response.output_item.added" {
			raw, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			firstAdded = raw
			break
		}
	}
	if firstAdded == nil || !strings.Contains(string(firstAdded), `"output_index":0`) {
		t.Fatalf("first output_item.added missing output_index 0: %s", firstAdded)
	}
	for _, e := range all {
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal event %s: %v", e.Type, err)
		}
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil || probe.Type != e.Type {
			t.Fatalf("event type roundtrip wrong: %s vs %s", probe.Type, e.Type)
		}
	}
	last := all[len(all)-1]
	if last.Response == nil || last.Response.Status != "completed" {
		t.Fatalf("completed frame wrong: %+v", last)
	}
	if len(last.Response.Output) == 0 || last.Response.Output[0].Content[0].Text != "Hello, world" {
		t.Fatalf("completed output wrong: %+v", last.Response.Output)
	}
}

func TestStreamBuilder_ReasoningThenMessage(t *testing.T) {
	b := NewResponseStreamBuilder("resp_t2", "auto", nil)
	var all []ResponseStreamEvent
	all = append(all, b.CreatedEvents()...)
	all = append(all, b.ReasoningStart()...)
	all = append(all, b.ReasoningDelta("thinking...")...)
	all = append(all, b.MessageStart()...)
	all = append(all, b.MessageDelta("answer")...)
	all = append(all, b.Completed(provider.Turn{Text: "answer", Thinking: "thinking..."})...)

	assertContainsInOrder(t, eventTypes(all), []string{
		"response.created", "response.in_progress",
		"response.output_item.added",
		"response.reasoning_summary_text.delta",
		"response.output_item.done",
		"response.output_item.added",
		"response.output_text.delta",
		"response.output_item.done",
		"response.completed",
	})
}

func TestStreamBuilder_ToolCall(t *testing.T) {
	b := NewResponseStreamBuilder("resp_t3", "auto", nil)
	var all []ResponseStreamEvent
	all = append(all, b.CreatedEvents()...)
	all = append(all, b.ToolCallStart("call_1", "shell")...)
	all = append(all, b.ToolCallDelta(`{"cmd":"ls"}`)...)
	all = append(all, b.ToolCallDone()...)
	all = append(all, b.Completed(provider.Turn{
		ToolUses: []provider.ToolUse{{ID: "call_1", Name: "shell", Input: map[string]any{"cmd": "ls"}}},
	})...)

	assertContainsInOrder(t, eventTypes(all), []string{
		"response.created", "response.in_progress",
		"response.output_item.added",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.done",
		"response.output_item.done",
		"response.completed",
	})

	// 严格客户端按 item_id 关联事件：added/delta/done 的 item id 必须一致。
	var addedItemID string
	var deltaItemID, doneArgsItemID string
	var argsDone string
	for _, e := range all {
		switch e.Type {
		case "response.output_item.added":
			if e.Item != nil && e.Item.Type == "function_call" {
				addedItemID = e.Item.ID
				if e.Item.CallID != "call_1" {
					t.Fatalf("added item call_id wrong: %+v", e.Item)
				}
			}
		case "response.function_call_arguments.delta":
			deltaItemID = e.ItemID
		case "response.function_call_arguments.done":
			doneArgsItemID = e.ItemID
			argsDone = e.Arguments
		}
	}
	if addedItemID == "" {
		t.Fatal("missing function_call output_item.added")
	}
	if deltaItemID != addedItemID || doneArgsItemID != addedItemID {
		t.Fatalf("item_id mismatch: added=%s delta=%s done=%s", addedItemID, deltaItemID, doneArgsItemID)
	}
	// arguments.done 必须携带完整 arguments 字段（官方规范），不能塞进 text。
	if argsDone != `{"cmd":"ls"}` {
		t.Fatalf("arguments.done payload wrong: %q", argsDone)
	}
	// output_item.done 里的 item id 也必须与 added 一致。
	for _, e := range all {
		if e.Type == "response.output_item.done" && e.Item != nil && e.Item.Type == "function_call" {
			if e.Item.ID != addedItemID {
				t.Fatalf("done item id %s != added id %s", e.Item.ID, addedItemID)
			}
			if e.Item.Arguments != `{"cmd":"ls"}` {
				t.Fatalf("done item arguments wrong: %q", e.Item.Arguments)
			}
		}
	}
}

func TestStreamBuilder_MessageThenToolCall(t *testing.T) {
	b := NewResponseStreamBuilder("resp_t4", "auto", nil)
	var all []ResponseStreamEvent
	all = append(all, b.CreatedEvents()...)
	all = append(all, b.MessageStart()...)
	all = append(all, b.MessageDelta("let me run")...)
	all = append(all, b.ToolCallStart("call_1", "shell")...)
	all = append(all, b.ToolCallDelta(`{"cmd":"ls"}`)...)
	all = append(all, b.Completed(provider.Turn{
		Text:     "let me run",
		ToolUses: []provider.ToolUse{{ID: "call_1", Name: "shell", Input: map[string]any{"cmd": "ls"}}},
	})...)

	assertContainsInOrder(t, eventTypes(all), []string{
		"response.output_item.added",
		"response.output_text.delta",
		"response.output_item.done",
		"response.output_item.added",
		"response.function_call_arguments.delta",
		"response.completed",
	})
}

func TestStreamBuilder_Failed(t *testing.T) {
	b := NewResponseStreamBuilder("resp_t5", "auto", nil)
	_ = b.CreatedEvents()
	ev := b.Failed("upstream_error", "boom")
	if ev.Type != "response.failed" {
		t.Fatalf("type wrong: %s", ev.Type)
	}
	if ev.Response == nil || ev.Response.Status != "failed" || ev.Response.Error == nil {
		t.Fatalf("failed response wrong: %+v", ev)
	}
	if ev.Response.Error.Message != "boom" {
		t.Fatalf("error message wrong: %+v", ev.Response.Error)
	}
}
