package admin_test

import (
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/admin"
)

// 面板语义体检：每个指标必须能回答「这是什么口径」。
//
// 背景：这里出现过真实的误读 —— 01 概览的「成功/失败」是进程内累计
// （重启清零），07 用量页的「请求数」是落盘统计（保留 90 天）。
// 两个页面并列显示却从不说明口径差异，看到 100 vs 80 只会以为是 bug。
//
// 这组测试锁住三类东西：
//  1. 需要口径标注的指标必须带 title
//  2. 语义与直觉不符的指标必须改名
//  3. 页面不得出现已知的误导性表述
func TestPanelMetricScopesAreDocumented(t *testing.T) {
	page := admin.PageHTML()
	for _, want := range []struct {
		needle string
		why    string
	}{
		{"本次启动以来累计", "01 概览的成功/失败是进程内累计，必须说明重启清零"},
		{"重启清零", "同上"},
		{"各分钟桶峰值的最大值", "并发条的 1h 峰值不是「最近 1 分钟」"},
		{"限流事件次数，不是账号数", "1h 限流计数是事件数"},
		{"只显示首个账号", "概览的 Credits 余额只来自 primary 账号"},
		{"缓存 token ÷ prompt token", "07 命中率的分母口径"},
		// 06 的 system 长度是 UTF-8 字节数（Go 的 len(string) 就是字节数）。
		{"UTF-8 字节数", "06 的 system 长度必须标注是字节数"},
		{"中文 system 的字节数约为字符数的 3 倍", "说明字节与字符的关系，避免误读"},
	} {
		if !strings.Contains(page, want.needle) {
			t.Errorf("缺少口径说明 %q（%s）", want.needle, want.why)
		}
	}
}

// 「1m 峰值」是个错误命名：后端返回的是最近一小时内每分钟桶的最大值。
func TestConcurrencyLabelsMatchBackendSemantics(t *testing.T) {
	page := admin.PageHTML()
	// 只判 id=ccPeak 那个标签：解释性文案里出现「1m」是允许的。
	if !strings.Contains(page, `>1h 峰值<b id="ccPeak">`) {
		t.Fatal("并发条的标签应为 1h 峰值（后端是 1h 内各分钟桶的最大值）")
	}
}

// 06 的 Stats 有 Stripped 字段，但页面从未展示 —— 加了剥离开关却看不到剥离次数。
func TestSystemPromptShowsStrippedCount(t *testing.T) {
	page := admin.PageHTML()
	for _, want := range []string{`id="spStripped"`, "已剥离"} {
		if !strings.Contains(page, want) {
			t.Errorf("06 面板缺少 %q —— Stats.Stripped 已存在，但界面上看不到剥离发生了多少次", want)
		}
	}
}

// 07 明细的 Credit 列是「单次请求扣费」，与 01 的「Credits 余额」是两个概念。
func TestUsageCreditColumnIsDistinguishedFromBalance(t *testing.T) {
	page := admin.PageHTML()
	if !strings.Contains(page, "单次 Credit") {
		t.Fatal("07 明细的 Credit 是单次扣费，须与 01 的余额区分命名")
	}
}

// rowTotalTokens 里 `row.totalTokens` 是死分支：usagejournal.Entry 没有该字段
// （只有 Summary / ModelStat / ChartPoint 有）。留着它会让后人以为字段存在。
func TestUsageRowTotalTokensHasNoDeadBranch(t *testing.T) {
	page := admin.PageHTML()
	// 只判代码里的实际读取：注释里提到字段名是允许的。
	if strings.Contains(page, "return row.totalTokens ||") {
		t.Fatal("Entry 没有 totalTokens 字段，这是死分支")
	}
}
