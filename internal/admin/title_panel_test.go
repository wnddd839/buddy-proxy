package admin_test

import (
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/admin"
)

// wb-lite 已删除（2026-11-22）。面板必须：
//  1. 不再出现 wb-lite 的下拉项与常量
//  2. 明确写出「未配置目标 ⇒ 实际不改写」
//
// 第 2 点尤其重要：这是最常见的「以为在省钱其实没有」状态，
// 不写清楚就会被当成 bug 排查。
func TestTitleRoutePanelHasNoWbLite(t *testing.T) {
	page := admin.PageHTML()
	for _, bad := range []string{
		"TITLE_LITE_MODEL",
		"wb-lite（隐藏轻量模型",
		"隐藏轻量模型 · 0 积分",
	} {
		if strings.Contains(page, bad) {
			t.Errorf("面板里不该再有 %q —— wb-lite 已删除", bad)
		}
	}
}

func TestTitleRoutePanelExplainsUnconfigured(t *testing.T) {
	page := admin.PageHTML()
	for _, want := range []string{
		"沿用请求模型（不改写）",
		"已启用但未配置目标",
		"实际不改写，标题仍由主模型生成",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("缺少 %q —— 开关开着但没配目标时，用户必须看到原因", want)
		}
	}
}
