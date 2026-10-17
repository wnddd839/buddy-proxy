package provider

import "testing"

func TestIsEmptyCompletedTurn(t *testing.T) {
	cases := []struct {
		name string
		turn Turn
		want bool
	}{
		{"stop with no text no tools (soft truncation)", Turn{StopReason: "stop"}, true},
		{"stop with thinking only (production evidence shape)", Turn{Thinking: "a very long reasoning...", StopReason: "stop"}, true},
		{"empty stream no finish reason", Turn{}, true},
		{"has text", Turn{Text: "pong", StopReason: "stop"}, false},
		{"whitespace text counts as empty", Turn{Text: "  \n ", StopReason: "stop"}, true},
		{"has tool calls", Turn{StopReason: "tool_calls", ToolUses: []ToolUse{{ID: "c1", Name: "run"}}}, false},
		{"content_filter is legitimate silence", Turn{StopReason: "content_filter"}, false},
		{"refusal is legitimate silence", Turn{StopReason: "refusal"}, false},
		{"CONTENT_FILTER case-insensitive", Turn{StopReason: "CONTENT_FILTER"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isEmptyCompletedTurn(tc.turn); got != tc.want {
				t.Fatalf("isEmptyCompletedTurn(%+v) = %v, want %v", tc.turn, got, tc.want)
			}
		})
	}
}

// 作者 P1：已交付（回调非空且事件被转发）的回合不判废。
func TestIsEmptyCompletedTurnDelivered(t *testing.T) {
	if isEmptyCompletedTurn(Turn{StopReason: "stop", Delivered: true}) {
		t.Fatal("delivered turn must not be judged empty")
	}
}

// 作者 P2：输出预算耗尽（length/max_tokens）无正文是合法完成。
func TestIsEmptyCompletedTurnLength(t *testing.T) {
	for _, reason := range []string{"length", "max_tokens", "LENGTH"} {
		if isEmptyCompletedTurn(Turn{StopReason: reason}) {
			t.Fatalf("finish_reason=%s must be a legitimate completion", reason)
		}
	}
}
