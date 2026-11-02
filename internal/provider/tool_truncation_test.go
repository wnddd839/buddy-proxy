package provider

import "testing"

func TestLooksTruncatedToolArgs(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{`{"cmd":"ls"}`, false},         // valid JSON: keep
		{`{"command": "echo hel`, true}, // truncated object
		{`[1,2`, true},                  // truncated array
		{"plain text arg", false},       // legacy non-JSON string: keep via fallback
		{`{"a":1,}`, false},             // malformed but complete: keep legacy fallback
		{`{'cmd':'ls'}`, false},         // single quotes: malformed but complete
		{`{"a": 1} extra`, false},       // trailing garbage: malformed but complete
	}
	for _, tc := range cases {
		if got := looksTruncatedToolArgs(tc.in); got != tc.want {
			t.Fatalf("looksTruncatedToolArgs(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSnapshotDropsTruncatedToolArgs(t *testing.T) {
	a := newAccumulator()
	a.push(Event{Type: "tool_call_delta", Index: 0, ID: "call_1", Name: "run", ArgumentsDelta: `{"command": "echo hel`})
	a.push(Event{Type: "tool_call_delta", Index: 0, ArgumentsDelta: "lo"})
	a.push(Event{Type: "turn_ended", StopReason: "stop"})
	turn := a.snapshot(func() string { return "" })
	if len(turn.ToolUses) != 0 {
		t.Fatalf("truncated tool args must be dropped, got %+v", turn.ToolUses)
	}
}

func TestSnapshotKeepsValidAndLegacyToolArgs(t *testing.T) {
	a := newAccumulator()
	a.push(Event{Type: "tool_call_delta", Index: 0, ID: "c1", Name: "run", ArgumentsDelta: `{"cmd":"ls"}`})
	a.push(Event{Type: "tool_call_delta", Index: 1, ID: "c2", Name: "echo", ArgumentsDelta: "plain text arg"})
	turn := a.snapshot(func() string { return "" })
	if len(turn.ToolUses) != 2 {
		t.Fatalf("valid and legacy args must survive, got %+v", turn.ToolUses)
	}
	// map 迭代顺序不确定：按工具名查找，不要按索引。
	byName := map[string]ToolUse{}
	for _, tu := range turn.ToolUses {
		byName[tu.Name] = tu
	}
	if got := byName["run"].Input["cmd"]; got != "ls" {
		t.Fatalf("parsed args lost: %+v", byName["run"].Input)
	}
	if got := byName["echo"].Input["input"]; got != "plain text arg" {
		t.Fatalf("legacy non-JSON args must keep old fallback: %+v", byName["echo"].Input)
	}
}
