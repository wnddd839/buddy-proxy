package admin_test

import (
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/admin"
)

// 06 的「词表身份鉴定」面板必须把三件事说清楚：
//  1. 判据是什么（差值为常数 ⇒ 官方词表）
//  2. **权重无法验证**（不能留白，留白会被读成「已验证通过」）
//  3. 成本是多少（会花 credit，用户有权先知道）
func TestTokIdentityPanelWiring(t *testing.T) {
	page := admin.PageHTML()
	for _, want := range []struct {
		needle string
		why    string
	}{
		{"词表身份鉴定", "区块标题"},
		{"prompt_tokens</code> 与官方", "判据来源"},
		{"差值恒为 4", "给出精确判据，避免读者自己猜"},
		{"差值各不相同", "反例：差值不一致即换了词表"},
		{"权重（模型本体）无法验证", "最关键的一行 —— 不能留白"},
		{"logprobs", "说明为什么无法验证"},
		{"蒸馏 / 微调模型可以继承词表", "解释「词表相同 ≠ 权重相同」"},
		{`id="btnTokIdCheck"`, "手动触发按钮"},
		{`id="tokIdTable"`, "结果表"},
		{"0.02 credit", "成本必须明示"},
		{"不设定时任务", "解释为什么只手动跑"},
	} {
		if !strings.Contains(page, want.needle) {
			t.Errorf("缺少 %q（%s）", want.needle, want.why)
		}
	}
}

// 结果表必须有「词表 / 权重 / 模板公式」三列 ——
// 只给一个总判定无法区分「换了词表」与「模板不同」。
func TestTokIdentityTableColumns(t *testing.T) {
	page := admin.PageHTML()
	for _, want := range []string{"词表", "权重", "模板公式", "样本明细"} {
		if !strings.Contains(page, want) {
			t.Errorf("结果表缺少列 %q", want)
		}
	}
}

// 校准失败时不能让页面显示「通过」—— 必须是「无法判定」。
func TestTokIdentityVerdictFallback(t *testing.T) {
	page := admin.PageHTML()
	if !strings.Contains(page, "无法判定") {
		t.Fatal("必须有「无法判定」这个兜底状态")
	}
	if !strings.Contains(page, "无法验证") {
		t.Fatal("权重必须有「无法验证」这个明确状态")
	}
}
