package modelroute

import "testing"

// 未配置目标时不该改写：标题由当前请求的主模型生成。
//
// 这一条来自用户明确要求：「默认目标就跟随实际请求的模型就行」。
// 旧行为是回落到 wb-lite（上游隐藏轻量模型），已删除。
func TestTitleRouteUnconfiguredDoesNotRewrite(t *testing.T) {
	route := &TitleRoute{Enabled: true}
	if got := route.TargetFor("domestic"); got != "" {
		t.Fatalf("未配置目标时应返回空（不改写），实际 %q", got)
	}
	if got := route.TargetFor("global"); got != "" {
		t.Fatalf("未配置目标时应返回空（不改写），实际 %q", got)
	}
}

// 只配了一个站点时，另一个站点也不该被改写到别处。
func TestTitleRouteSingleSiteOtherDoesNotRewrite(t *testing.T) {
	route := &TitleRoute{Enabled: true, Domestic: "hy4flash-free"}
	if got := route.TargetFor("domestic"); got != "hy4flash-free" {
		t.Fatalf("国内站应尊重显式配置：%q", got)
	}
	if got := route.TargetFor("global"); got != "" {
		t.Fatalf("国际站未配置时不该改写，实际 %q", got)
	}
}

// 开关关闭时任何配置都不生效。
func TestTitleRouteDisabledReturnsEmpty(t *testing.T) {
	route := &TitleRoute{Enabled: false, Domestic: "hy4flash-free", Global: "hy4flash-free"}
	if got := route.TargetFor("domestic"); got != "" {
		t.Fatalf("开关关闭时应返回空，实际 %q", got)
	}
}

func TestTitleRouteExplicitTargetWins(t *testing.T) {
	route := &TitleRoute{
		Enabled:  true,
		Domestic: "hy4flash-free",
		Global:   "deepseek-v4.1-flash",
	}
	if got := route.TargetFor("domestic"); got != "hy4flash-free" {
		t.Fatalf("国内站应尊重显式配置：%q", got)
	}
	if got := route.TargetFor("global"); got != "deepseek-v4.1-flash" {
		t.Fatalf("国际站应尊重显式配置：%q", got)
	}
}

// ValidateTitleRoute 不再要求「必须是 wb-lite」。
func TestValidateTitleRouteAcceptsAnyModel(t *testing.T) {
	for _, tc := range []TitleRoute{
		{Enabled: true, Domestic: "hy4flash-free"},
		{Enabled: true, Global: "deepseek-v4.1-flash"},
		{Enabled: true, Domestic: "custom-model-x"},
		{Enabled: true}, // 未配置也合法（= 不改写）
	} {
		if err := ValidateTitleRoute(&tc); err != nil {
			t.Errorf("%+v 应合法：%v", tc, err)
		}
	}
}
