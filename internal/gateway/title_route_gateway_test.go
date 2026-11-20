package gateway

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/modelroute"
)

// 标题改写只改模型名：两站点各配一个目标，请求落在哪个站就用哪个。
func TestTitleRoutePerSiteTargets(t *testing.T) {
	route := &modelroute.TitleRoute{
		Enabled:  true,
		Domestic: "hy4flash-free",
		Global:   "deepseek-v4.1-flash",
	}
	if err := modelroute.ValidateTitleRoute(route); err != nil {
		t.Fatalf("配置应合法：%v", err)
	}
	if got := route.TargetFor("domestic"); got != "hy4flash-free" {
		t.Fatalf("国内站目标错：%q", got)
	}
	if got := route.TargetFor("global"); got != "deepseek-v4.1-flash" {
		t.Fatalf("国际站目标错：%q", got)
	}
}

// 关闭时不得改写：默认状态，也是回滚路径。
func TestTitleRouteDisabledByDefault(t *testing.T) {
	store := modelroute.Store{}
	if store.TitleRoute != nil {
		t.Fatal("零值 Store 的 TitleRoute 应为 nil")
	}
	if err := modelroute.ValidateTitleRoute(store.TitleRoute); err != nil {
		t.Fatalf("nil 配置不该报错：%v", err)
	}
}

// 启用但两站都没目标必须被拒。
func TestTitleRouteRequiresAtLeastOneTarget(t *testing.T) {
	if err := modelroute.ValidateTitleRoute(&modelroute.TitleRoute{Enabled: true}); err == nil {
		t.Fatal("启用但无目标时必须报错")
	}
}

// 只配一个站点是合法的：另一个站点保持原模型。
func TestTitleRouteAllowsSingleSite(t *testing.T) {
	route := &modelroute.TitleRoute{Enabled: true, Domestic: "hy4flash-free"}
	if err := modelroute.ValidateTitleRoute(route); err != nil {
		t.Fatalf("只配国内站应合法：%v", err)
	}
	if got := route.TargetFor("global"); got != "" {
		t.Fatalf("未配置的国际站应不改写，实际 %q", got)
	}
}

// 标题请求识别：只认「单条 user + 无 assistant/tool + 模板开头」。
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
