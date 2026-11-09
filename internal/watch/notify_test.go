package watch

import (
	"strings"
	"testing"
	"time"
)

func TestRenderMailKeepsToFacts(t *testing.T) {
	// 邮件正文只保留可核验的指标，不再内置「对号入座」式的推测文案。
	// 那些推测在多数场景下是错的（例如把上游正常降档说成"推理字段缺失"），
	// 反而误导排查；细节去管理台看。
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
		t.Fatalf("subject should carry the reason, got %q", subject)
	}
	for _, want := range []string{"错误率", "26.0%", "3100ms", "https://gw.example.com/direct-admin/"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
	// 不应再出现推测性文案。
	for _, unwanted := range []string{"对号入座", "403 code 11140", "不是网关故障"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("body should not guess causes (%q):\n%s", unwanted, body)
		}
	}
}

func TestRenderMailWithoutAdminURL(t *testing.T) {
	_, body := renderMail(Event{Kind: "remind", Reason: "推理占比偏高", At: time.Now().UnixMilli()}, "")
	if strings.Contains(body, "管理台") {
		t.Fatalf("should not mention admin console when URL is empty:\n%s", body)
	}
	// 也要能正常渲染出指标，不能因为缺 URL 就变成空邮件。
	if !strings.Contains(body, "错误率") {
		t.Fatalf("body should still carry metrics:\n%s", body)
	}
}

func TestRenderMailRecoverTaggings(t *testing.T) {
	sub, _ := renderMail(Event{Kind: "recover", Reason: "推理字段缺失", At: time.Now().UnixMilli(), Since: time.Now().Add(-time.Hour).UnixMilli()}, "")
	if !strings.Contains(sub, "恢复") {
		t.Fatalf("recover subject should be tagged, got %q", sub)
	}
}
