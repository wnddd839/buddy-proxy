package watch

import (
	"strings"
	"testing"
	"time"
)

func TestRenderMailNamesTheRealCause(t *testing.T) {
	subject, body := renderMail(Event{
		Kind:   "alert",
		Reason: "链路/上游请求失败",
		At:     time.Now().UnixMilli(),
		Metrics: Metrics{
			Samples: 120, ErrorRate: 0.26,
			ReasoningAsked: 0, ReasoningMissRate: 0,
			LatencyP50: 3100, BaselineLatencyP50: 2900,
		},
	}, "https://gw.example.com/direct-admin/")

	if !strings.Contains(subject, "链路/上游请求失败") {
		t.Fatalf("subject should name the cause, got %q", subject)
	}
	// 邮件必须能自证：包含管理台入口与「对号入座」的分类，避免把链路问题
	// 报成「思考字段消失」。
	for _, want := range []string{
		"https://gw.example.com/direct-admin/",
		"403 code 11140",
		"不是网关故障",
		"换一个问法",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
}

func TestRenderMailWithoutAdminURL(t *testing.T) {
	_, body := renderMail(Event{Kind: "remind", Reason: "推理字段缺失", At: time.Now().UnixMilli()}, "")
	if strings.Contains(body, "管理台概览页") {
		t.Fatalf("should not mention admin console when URL is empty:\n%s", body)
	}
	if !strings.Contains(body, "journalctl") {
		t.Fatalf("should still give a usable entry point:\n%s", body)
	}
}

func TestRenderMailRecoverTaggings(t *testing.T) {
	sub, _ := renderMail(Event{Kind: "recover", Reason: "推理字段缺失", At: time.Now().UnixMilli(), Since: time.Now().Add(-time.Hour).UnixMilli()}, "")
	if !strings.Contains(sub, "恢复") {
		t.Fatalf("recover subject should be tagged, got %q", sub)
	}
}
