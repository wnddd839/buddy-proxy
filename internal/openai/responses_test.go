package openai

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

func TestResponsesRequestToMessages_StringInput(t *testing.T) {
	req := &ResponsesRequest{Model: "auto", Instructions: "You are a coding agent.", Input: "hello"}
	messages, _, _ := req.ToMessages()
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0]["role"] != "system" || messages[0]["content"] != "You are a coding agent." {
		t.Fatalf("system message wrong: %+v", messages[0])
	}
	if messages[1]["role"] != "user" || messages[1]["content"] != "hello" {
		t.Fatalf("user message wrong: %+v", messages[1])
	}
}

func TestResponsesRequestToMessages_ItemArray(t *testing.T) {
	raw := `{
		"model": "auto",
		"input": [
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]},
			{"type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"cmd\":\"ls\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"file.txt"}
		]
	}`
	var req ResponsesRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	messages, _, _ := req.ToMessages()
	if len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %d: %+v", len(messages), messages)
	}
	if messages[0]["role"] != "user" || messages[0]["content"] != "hi" {
		t.Fatalf("user msg wrong: %+v", messages[0])
	}
	if messages[1]["role"] != "assistant" || messages[1]["content"] != "hello" {
		t.Fatalf("assistant msg wrong: %+v", messages[1])
	}
	fc := messages[2]
	if fc["role"] != "assistant" {
		t.Fatalf("function_call role wrong: %+v", fc)
	}
	calls, ok := fc["tool_calls"].([]map[string]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("tool_calls wrong: %+v", fc["tool_calls"])
	}
	fn := calls[0]["function"].(map[string]any)
	if calls[0]["id"] != "call_1" || fn["name"] != "shell" {
		t.Fatalf("function_call content wrong: %+v", calls[0])
	}
	out := messages[3]
	if out["role"] != "tool" || out["tool_call_id"] != "call_1" || out["content"] != "file.txt" {
		t.Fatalf("function_call_output wrong: %+v", out)
	}
}

func TestResponsesToolRoundTripSurvivesEnsureUpstreamMessages(t *testing.T) {
	raw := `{
		"model": "auto",
		"instructions": "be brief",
		"input": [
			{"type":"message","role":"user","content":[{"type":"input_text","text":"run ls"}]},
			{"type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"cmd\":\"ls\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"file.txt"}
		]
	}`
	var req ResponsesRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	messages, _, _ := req.ToMessages()
	out := provider.EnsureUpstreamMessages(messages)
	var roles []string
	var assistantCalls []any
	var toolIDs []string
	for _, msg := range out {
		role := fmt.Sprint(msg["role"])
		roles = append(roles, role)
		if role == "assistant" {
			if tc, ok := msg["tool_calls"].([]any); ok {
				assistantCalls = tc
			}
		}
		if role == "tool" {
			toolIDs = append(toolIDs, fmt.Sprint(msg["tool_call_id"]))
		}
	}
	if len(assistantCalls) != 1 {
		t.Fatalf("assistant tool_calls dropped; roles=%v out=%+v", roles, out)
	}
	call, _ := assistantCalls[0].(map[string]any)
	if call["id"] != "call_1" {
		t.Fatalf("tool call id=%v", call["id"])
	}
	if len(toolIDs) != 1 || toolIDs[0] != "call_1" {
		t.Fatalf("tool results=%v roles=%v", toolIDs, roles)
	}
}

func TestResponsesRequestToMessages_DeveloperRole(t *testing.T) {
	raw := `{"model":"auto","input":[{"type":"message","role":"developer","content":[{"type":"input_text","text":"be terse"}]}]}`
	var req ResponsesRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	messages, _, _ := req.ToMessages()
	if len(messages) != 1 || messages[0]["role"] != "system" {
		t.Fatalf("developer role not mapped to system: %+v", messages)
	}
}

func TestResponsesToolsFlatToChat(t *testing.T) {
	raw := `{"model":"auto","input":"x","tools":[{"type":"function","name":"shell","description":"run cmd","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}]}`
	var req ResponsesRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	_, tools, _ := req.ToMessages()
	list, ok := tools.([]map[string]any)
	if !ok || len(list) != 1 {
		t.Fatalf("tools wrong: %+v", tools)
	}
	if list[0]["type"] != "function" {
		t.Fatalf("tool type wrong: %+v", list[0])
	}
	fn := list[0]["function"].(map[string]any)
	if fn["name"] != "shell" || fn["description"] != "run cmd" {
		t.Fatalf("function def wrong: %+v", fn)
	}
	params := fn["parameters"].(map[string]any)
	if params["type"] != "object" {
		t.Fatalf("parameters wrong: %+v", params)
	}
}

func TestResponsesToolChoiceObject(t *testing.T) {
	raw := `{"model":"auto","input":"x","tool_choice":{"type":"function","name":"shell"}}`
	var req ResponsesRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	_, _, choice := req.ToMessages()
	m, ok := choice.(map[string]any)
	if !ok {
		t.Fatalf("choice wrong: %+v", choice)
	}
	fn := m["function"].(map[string]any)
	if m["type"] != "function" || fn["name"] != "shell" {
		t.Fatalf("choice content wrong: %+v", m)
	}
}

func TestResponseFromTurn(t *testing.T) {
	turn := provider.Turn{
		Text:     "done",
		Thinking: "let me think",
		ToolUses: []provider.ToolUse{{ID: "call_9", Name: "shell", Input: map[string]any{"cmd": "ls"}}},
		Usage:    provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
	req := &ResponsesRequest{Model: "auto", Input: "hi"}
	obj := ResponseFromTurn(turn, "resp_1", "auto", req)
	if obj.Object != "response" || obj.Status != "completed" || obj.ID != "resp_1" {
		t.Fatalf("response head wrong: %+v", obj)
	}
	if len(obj.Output) != 3 {
		t.Fatalf("expected reasoning+message+function_call, got %d", len(obj.Output))
	}
	if obj.Output[0].Type != "reasoning" {
		t.Fatalf("first item should be reasoning: %+v", obj.Output[0])
	}
	if len(obj.Output[0].Summary) == 0 {
		t.Fatal("reasoning summary missing thinking text")
	}
	if obj.Output[1].Type != "message" || obj.Output[1].Content[0].Text != "done" {
		t.Fatalf("message item wrong: %+v", obj.Output[1])
	}
	fc := obj.Output[2]
	if fc.Type != "function_call" || fc.CallID != "call_9" || fc.Name != "shell" {
		t.Fatalf("function_call item wrong: %+v", fc)
	}
	if !strings.Contains(fc.Arguments, `"cmd":"ls"`) {
		t.Fatalf("arguments wrong: %s", fc.Arguments)
	}
	if obj.Usage == nil || obj.Usage.InputTokens != 10 || obj.Usage.OutputTokens != 5 {
		t.Fatalf("usage wrong: %+v", obj.Usage)
	}
	seen := map[string]bool{}
	for _, item := range obj.Output {
		if item.ID == "" {
			t.Fatalf("item missing id: %+v", item)
		}
		if seen[item.ID] {
			t.Fatalf("duplicate item id %s", item.ID)
		}
		seen[item.ID] = true
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"object":"response"`) {
		t.Fatalf("serialized response missing object=response: %s", raw)
	}
}

func TestResponsesUsageFromProvider(t *testing.T) {
	u := provider.Usage{
		PromptTokens:            100,
		CompletionTokens:        40,
		TotalTokens:             140,
		PromptTokensDetails:     &provider.PromptTokensDetails{CachedTokens: 30},
		CompletionTokensDetails: &provider.CompletionTokensDetails{ReasoningTokens: 12},
	}
	out := ResponsesUsageFromProvider(u)
	if out.InputTokens != 100 || out.OutputTokens != 40 || out.TotalTokens != 140 {
		t.Fatalf("usage wrong: %+v", out)
	}
	if out.InputTokensDetails.CachedTokens != 30 {
		t.Fatalf("cached tokens wrong: %+v", out.InputTokensDetails)
	}
	if out.OutputTokensDetails.ReasoningTokens != 12 {
		t.Fatalf("reasoning tokens wrong: %+v", out.OutputTokensDetails)
	}
}

func TestNewItemIDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := NewItemID("msg")
		if !strings.HasPrefix(id, "msg_") {
			t.Fatalf("prefix wrong: %s", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %s at %d", id, i)
		}
		seen[id] = true
	}
}
