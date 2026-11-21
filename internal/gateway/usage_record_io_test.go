package gateway

import (
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

// RecordChat 必须把新字段带进 journal（否则 I3/I4 在生产里永远看到零值）。
func TestRecordChatCarriesIOAndFlags(t *testing.T) {
	svc := New(config.Config{Site: "domestic"}, nil)
	t.Cleanup(func() { _ = svc.Close() })

	svc.RecordChat(ChatRecordInput{
		Started:        time.Now(),
		Model:          "deepseek-v4.1-flash",
		Ok:             true,
		TitleRewrite:   true,
		UsageSource:    "upstream",
		SystemForm:     "keep",
		SystemFp:       "ab12cd34",
		SystemStripped: true,
		StripScope:     "system",
		IO: &provider.MessagesIO{
			InMessages: 5, OutMessages: 5,
			InBytes: 900, OutBytes: 800,
			InSystemLen: 100, OutSystemLen: 100,
		},
	})

	reqs := svc.Journal.View("day", 10, 0).Requests
	if len(reqs) != 1 {
		t.Fatalf("应有 1 条用量记录，实际 %d", len(reqs))
	}
	row := reqs[0]
	if row.SystemLen != 100 {
		t.Errorf("SystemLen = %d, want 100", row.SystemLen)
	}
	if !row.SystemStripped || row.StripScope != "system" {
		t.Errorf("剥离标记没落盘：%+v", row)
	}
	if row.InMessages != 5 || row.OutMessages != 5 || row.InBytes != 900 || row.OutBytes != 800 {
		t.Errorf("IO 统计没落盘：%+v", row)
	}
	if !row.TitleRewrite {
		t.Error("TitleRewrite 没落盘")
	}
	if row.UsageSource != "upstream" {
		t.Errorf("UsageSource = %q", row.UsageSource)
	}
}

// 未观测 IO 时全部为零值，且 UsageSource 归一到 upstream。
func TestRecordChatWithoutIO(t *testing.T) {
	svc := New(config.Config{Site: "domestic"}, nil)
	t.Cleanup(func() { _ = svc.Close() })

	svc.RecordChat(ChatRecordInput{Started: time.Now(), Model: "m", Ok: true})
	reqs := svc.Journal.View("day", 10, 0).Requests
	if len(reqs) != 1 {
		t.Fatalf("应有 1 条，实际 %d", len(reqs))
	}
	row := reqs[0]
	if row.InMessages != 0 || row.OutMessages != 0 {
		t.Errorf("未观测时应为 0：%+v", row)
	}
	if row.UsageSource != "upstream" {
		t.Errorf("空值应归一为 upstream，实际 %q", row.UsageSource)
	}
}

// systemIOFor 必须在同一个 SystemOutcome 上复用同一个指针，
// 否则重试时会重复分配、且后一轮会覆盖前一轮的统计。
func TestSystemIOForReusesPointer(t *testing.T) {
	outcome := &SystemPromptOutcome{}
	first := systemIOFor(CompleteOptions{SystemOutcome: outcome})
	second := systemIOFor(CompleteOptions{SystemOutcome: outcome})
	if first == nil || first != second {
		t.Fatalf("应复用同一个指针：%p vs %p", first, second)
	}
	if got := systemIOFor(CompleteOptions{}); got != nil {
		t.Fatalf("没有 SystemOutcome 时应返回 nil")
	}
}
