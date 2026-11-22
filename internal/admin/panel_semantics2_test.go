package admin_test

import (
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/admin"
)

// 第二批面板语义体检：02 / 03 / 04 / 05。
//
// 每一项都对应一处真实的口径混淆，不是文案润色：
//   - 02 的 success/fail 是账号历史累计（落盘），01 是进程内累计（重启清零）
//   - 02 的「已登录」只代表有凭据，不代表 token 没过期
//   - 02 的选号门槛按 6 小时内的快照判定，过期即放行
//   - 03 的签到统计按北京时间自然日
//   - 03 的「规则数」是本地草稿，不是已生效配置
//   - 04 的三个 URL 是并列端点，不是「Base 加路径」
//   - 05 的编目 / 路由 / 标题改写是三件互不依赖的事
func TestPanelSemanticsRound2(t *testing.T) {
	page := admin.PageHTML()
	for _, want := range []struct {
		needle string
		why    string
	}{
		// 02
		{"该账号<strong>历史累计</strong>", "02 的 success/fail 与 01 的口径不同，必须说明"},
		{"重启不清零", "02 的计数落盘，与 01 相反"},
		{"只表示", "「已登录」徽标只代表有凭据"},
		{"快照 6 小时内有效", "选号门槛依赖快照时效，过期放行"},
		// 03
		{"北京时间", "签到按北京时间的自然日统计"},
		{"本地草稿", "规则数是草稿而非已生效配置"},
		{"保存始终包含两站全部规则", "范围只影响统计与批量操作，不影响保存范围"},
		// 04
		{"并列的三个端点", "Base / Chat / Responses 不是包含关系"},
		// 05
		{"本页三块<strong>互不依赖</strong>", "编目 / 路由 / 标题改写是三件不同的事"},
		{"从两个站各拉一份", "编目是拉取上游真实模型列表"},
		{"你<strong>额外配置</strong>的改写规则", "路由是用户配置，不是上游事实"},
	} {
		if !strings.Contains(page, want.needle) {
			t.Errorf("缺少 %q（%s）", want.needle, want.why)
		}
	}
}

// 「1m 峰值」这类误导性命名不得再出现在标签位置。
func TestNoMisleadingPeakLabel(t *testing.T) {
	page := admin.PageHTML()
	if strings.Contains(page, `>1m 峰值<b`) {
		t.Fatal("并发条标签不得是「1m 峰值」")
	}
}

// 06 的 system 长度列必须标明是字符数（不是字节数）。
func TestSystemLenColumnSaysChars(t *testing.T) {
	page := admin.PageHTML()
	if strings.Contains(page, "<th>system 字节</th>") {
		t.Fatal("06 的列名不得写「system 字节」—— 实际统计的是字符数")
	}
	if !strings.Contains(page, "system 长度") {
		t.Fatal("06 的列名应为「system 长度」")
	}
}
