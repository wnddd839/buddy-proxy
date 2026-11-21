package provider_test

import (
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

func TestFillMessagesIOCountsAndBytes(t *testing.T) {
	rep := &provider.MessagesIO{}
	in := []map[string]any{
		{"role": "system", "content": strings.Repeat("s", 100)},
		{"role": "user", "content": "hi"},
	}
	out := provider.EnsureUpstreamMessagesWithReplay(in, true, nil, nil, true)
	provider.FillMessagesIO(rep, in, out)

	if rep.InMessages != 2 || rep.OutMessages != 2 {
		t.Fatalf("条数不对：%+v", rep)
	}
	if rep.InSystemLen != 100 || rep.OutSystemLen != 100 {
		t.Fatalf("system 长度不对：%+v", rep)
	}
	if rep.InBytes != 102 {
		t.Fatalf("InBytes 应为 100+2，实际 %d", rep.InBytes)
	}
}

// content 为数组形态（多段 text）时也要算进去。
func TestFillMessagesIOHandlesContentArray(t *testing.T) {
	rep := &provider.MessagesIO{}
	msgs := []map[string]any{{
		"role": "user",
		"content": []any{
			map[string]any{"type": "text", "text": "abc"},
			map[string]any{"type": "text", "text": "de"},
		},
	}}
	provider.FillMessagesIO(rep, msgs, msgs)
	if rep.InBytes != 5 || rep.OutBytes != 5 {
		t.Fatalf("数组内容未计入：%+v", rep)
	}
}

// fold 后 system 位为空：这是合法策略，I4 靠 form 豁免；
// 但 IO 统计必须如实反映「出站 system 0 字节」。
func TestFoldShowsSystemMovedOut(t *testing.T) {
	rep := &provider.MessagesIO{}
	in := []map[string]any{
		{"role": "system", "content": "SYS"},
		{"role": "user", "content": "hi"},
	}
	out := []map[string]any{{"role": "user", "content": "SYS\n\nhi"}}
	provider.FillMessagesIO(rep, in, out)
	if rep.OutSystemLen != 0 {
		t.Fatalf("fold 后出站不该有 system：%+v", rep)
	}
	if rep.OutMessages != 1 || rep.InMessages != 2 {
		t.Fatalf("%+v", rep)
	}
}

// nil dst 不得 panic（生产里很多调用方不关心 IO）。
func TestFillMessagesIONilSafe(t *testing.T) {
	provider.FillMessagesIO(nil, nil, nil)
}
