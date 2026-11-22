package gateway

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/modelroute"
)

// 标题路由的网关层接线：默认不改写、显式配置才改写。
//
// 旧版这三处断言「未配置回落 wb-lite」，wb-lite 已删除（2026-11-22），
// 默认行为改为「跟随实际请求的模型」= 不配置即不改写。
func TestTitleRouteDefaultsToNoRewrite(t *testing.T) {
	route := &modelroute.TitleRoute{Enabled: true}
	if got := route.TargetFor("domestic"); got != "" {
		t.Fatalf("未配置时不该改写，实际 %q", got)
	}
	if got := route.TargetFor("global"); got != "" {
		t.Fatalf("未配置时不该改写，实际 %q", got)
	}
}

// 显式配置时按配置走。
func TestTitleRouteExplicitTarget(t *testing.T) {
	route := &modelroute.TitleRoute{Enabled: true, Domestic: "hy4flash-free"}
	if got := route.TargetFor("domestic"); got != "hy4flash-free" {
		t.Fatalf("应尊重显式配置，实际 %q", got)
	}
}

// 开关关闭时不改写（即使配了目标）。
func TestTitleRouteDisabled(t *testing.T) {
	route := &modelroute.TitleRoute{
		Enabled: false, Domestic: "hy4flash-free", Global: "hy4flash-free",
	}
	if got := route.TargetFor("domestic"); got != "" {
		t.Fatalf("开关关闭时不该改写，实际 %q", got)
	}
}
