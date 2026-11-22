package admin_test

import (
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/admin"
)

// I8 在面板上的三个落点：
//  1. 07 明细新增「上游模型」列（与「模型」列并排对照）
//  2. 05 模型页新增「上游模型回显对照」区块（按周统计 + 替换明细）
//  3. upstreamModelCell 的三种状态渲染（未观测 / 一致 / 替换 / auto 解析）
func TestModelEchoPanelWiring(t *testing.T) {
	page := admin.PageHTML()
	for _, want := range []struct {
		needle string
		why    string
	}{
		{"上游模型", "07 明细需要一列展示上游回显的模型"},
		{"上游 SSE 帧里回显的 model 字段", "该列的口径必须写清楚，否则会被当成「我们发的模型」"},
		{"上游模型回显对照", "05 需要一个区块整体回答「有没有被换模型」"},
		{"归一化后比较", "说明 hy4flash-free 与 hy4-flash-free 是同一模型，避免误读"},
		{"auto 一族由上游解析", "auto 由上游解析，不算替换"},
		{`id="echoMismatch"`, "不一致数是最关键的那个指标"},
		{`id="echoTotal"`, "要有分母，否则「0 不一致」无从判断"},
		{`id="btnRefreshEcho"`, "需要手动刷新入口"},
		{"upstreamModelCell", "07 的单元格渲染函数"},
		{"模型被替换", "替换时的 tooltip 必须说清楚差在哪"},
	} {
		if !strings.Contains(page, want.needle) {
			t.Errorf("缺少 %q（%s）", want.needle, want.why)
		}
	}
}

// 05 的对照区块必须有「一致 / 不一致 / 上游解析」三个分类，
// 只报总数无法区分「没被换」与「auto 本来就不同」。
func TestModelEchoHasThreeBuckets(t *testing.T) {
	page := admin.PageHTML()
	for _, id := range []string{"echoMatch", "echoMismatch", "echoAuto"} {
		if !strings.Contains(page, `id="`+id+`"`) {
			t.Fatalf("缺少分类计数 %s", id)
		}
	}
}

// 07 的「上游模型」列位置：必须在「模型」之后、「单次 Credit」之前，
// 这样三列（客户端要的 / 上游回显 / 扣费）在同一屏内可对照。
func TestUpstreamModelColumnOrder(t *testing.T) {
	page := admin.PageHTML()
	model := strings.Index(page, "<th>模型</th>")
	upstream := strings.Index(page, "上游模型</th>")
	credit := strings.Index(page, "单次 Credit</th>")
	if model < 0 || upstream < 0 || credit < 0 {
		t.Fatalf("三列表头未找齐: model=%d upstream=%d credit=%d", model, upstream, credit)
	}
	if !(model < upstream && upstream < credit) {
		t.Fatalf("列序应为 模型 → 上游模型 → 单次 Credit，实际 %d/%d/%d", model, upstream, credit)
	}
}
