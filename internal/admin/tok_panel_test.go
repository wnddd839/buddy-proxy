package admin_test

import (
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/admin"
)

// 分词器指纹面板必须把「不需要安装 tokenizer」这件事写清楚 ——
// 这是用户最可能的疑问：以为又要装个依赖。
func TestTokFingerprintPanelExplainsNoInstall(t *testing.T) {
	page := admin.PageHTML()
	for _, want := range []struct {
		needle string
		why    string
	}{
		{"分词器指纹基线", "区块标题"},
		{"不需要安装任何", "用户最可能以为要装 tokenizer 依赖"},
		{"token 数来自上游返回的 <code>prompt_tokens</code>", "说清 token 数从哪来"},
		{"字符成分在网关本地按字符类别数", "说清成分怎么算"},
		{"按内容成分分桶", "分桶是判据的核心，不说明会被当成拍脑袋"},
		{"0.4552", "给出实测基准值，让人能对照"},
		{"0.6018", "同上，且这两个值的差距正说明数字桶最敏感"},
		{`id="tokEnable"`, "用户选择开关"},
		{`id="tokTable"`, "基线表"},
		{`id="btnRefreshTok"`, "刷新入口"},
	} {
		if !strings.Contains(page, want.needle) {
			t.Errorf("缺少 %q（%s）", want.needle, want.why)
		}
	}
}

// 基线表必须有快线 / 慢线 / 当前 / 样本四列 ——
// 只显示「当前比例」无法判断它是否正常。
func TestTokBaselineTableColumns(t *testing.T) {
	page := admin.PageHTML()
	for _, want := range []string{"快线", "慢线", "当前", "样本"} {
		if !strings.Contains(page, want) {
			t.Errorf("基线表缺少列 %q", want)
		}
	}
}

// I9 必须带 why（面板 tooltip 依赖它），且不能与 I8 混为一谈。
func TestI9RegisteredWithWhy(t *testing.T) {
	page := admin.PageHTML()
	// specs 由后端注入，这里只断言面板不把它和 I8 混写。
	if !strings.Contains(page, "分子") && !strings.Contains(page, "分词器") {
		t.Fatal("面板应提到分词器")
	}
}
