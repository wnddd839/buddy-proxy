package gateway

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/modelroute"
)

// 默认目标：启用后未配置的站点一律改写到 wb-lite。
func TestTitleRouteDefaultsToLite(t *testing.T) {
	route := &modelroute.TitleRoute{Enabled: true}
	if err := modelroute.ValidateTitleRoute(route); err != nil {
		t.Fatalf("启用但无显式目标应合法：%v", err)
	}
	if got := route.TargetFor("domestic"); got != modelroute.LiteModel {
		t.Fatalf("国内站默认应为 wb-lite，实际 %q", got)
	}
	if got := route.TargetFor("global"); got != modelroute.LiteModel {
		t.Fatalf("国际站默认应为 wb-lite，实际 %q", got)
	}
}

// 显式配置优先于默认。
func TestTitleRouteExplicitWins(t *testing.T) {
	route := &modelroute.TitleRoute{
		Enabled:  true,
		Domestic: "hy4flash-free",
		Global:   "deepseek-v4.1-flash",
	}
	if got := route.TargetFor("domestic"); got != "hy4flash-free" {
		t.Fatalf("国内站目标错：%q", got)
	}
	if got := route.TargetFor("global"); got != "deepseek-v4.1-flash" {
		t.Fatalf("国际站目标错：%q", got)
	}
}

// 关闭时不改写，即使配了目标 —— 这是回滚路径。
func TestTitleRouteDisabledNeverRewrites(t *testing.T) {
	store := modelroute.Store{}
	if store.TitleRoute != nil {
		t.Fatal("零值 Store 的 TitleRoute 应为 nil")
	}
	route := &modelroute.TitleRoute{
		Enabled: false, Domestic: modelroute.LiteModel, Global: modelroute.LiteModel,
	}
	if got := route.TargetFor("domestic"); got != "" {
		t.Fatalf("未启用时不该返回目标：%q", got)
	}
}

// 标题请求识别边界。
func TestTitleRouteDetectionBoundary(t *testing.T) {
	title := []map[string]any{
		{"role": "user", "content": modelroute.SessionTitlePrefix + " []"},
	}
	if !modelroute.IsSessionTitleRequest(title) {
		t.Fatal("标准标题请求应被识别")
	}
	normal := []map[string]any{{"role": "user", "content": "帮我写个函数"}}
	if modelroute.IsSessionTitleRequest(normal) {
		t.Fatal("普通对话不该被识别")
	}
}
