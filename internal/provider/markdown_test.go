package provider

import (
	"strings"
	"testing"
)

func TestATXFixerInsertsSpace(t *testing.T) {
	f := newATXFixer()
	got := f.Feed("##验证四个文件 php")
	if got != "## 验证四个文件 php" {
		t.Fatalf("got %q", got)
	}
}

func TestATXFixerLeavesSingleHash(t *testing.T) {
	f := newATXFixer()
	in := "#include <stdio.h>\n#!/bin/bash\n#define FOO"
	if got := f.Feed(in); got != in {
		t.Fatalf("single # must stay: %q", got)
	}
}

func TestATXFixerPreservesValidHeading(t *testing.T) {
	f := newATXFixer()
	in := "## 已经有空格\n### 也有"
	if got := f.Feed(in); got != in {
		t.Fatalf("got %q", got)
	}
}

func TestATXFixerAcrossChunks(t *testing.T) {
	f := newATXFixer()
	a := f.Feed("前文。\n")
	b := f.Feed("##")
	c := f.Feed("验证四个文件")
	got := a + b + c
	if got != "前文。\n## 验证四个文件" {
		t.Fatalf("got %q", got)
	}
}

func TestFirstTextKeepsNewlineDelta(t *testing.T) {
	if got := firstText("\n"); got != "\n" {
		t.Fatalf("newline delta dropped: %q", got)
	}
	if got := firstText(""); got != "" {
		t.Fatalf("empty should stay empty, got %q", got)
	}
	chunk := openAISSEChunk{Choices: []openAISSEChoice{{
		Delta: &openAISSEDelta{Content: "\n"},
	}}}
	events := eventsFromOpenAIChunk(chunk)
	if len(events) != 1 || events[0].Text != "\n" {
		t.Fatalf("events=%+v", events)
	}
}

func TestFixMarkdownFromSplitSSE(t *testing.T) {
	f := newATXFixer()
	var out strings.Builder
	for _, part := range []string{"未能执行。", "\n", "##验证四个文件", " 全部通过。"} {
		text := firstText(part)
		if text == "" {
			t.Fatalf("dropped %q", part)
		}
		out.WriteString(f.Feed(text))
	}
	got := out.String()
	if !strings.Contains(got, "\n## 验证四个文件") {
		t.Fatalf("markdown heading broken: %q", got)
	}
}
