package modelroute

import "testing"

// wb-lite 是隐藏模型，不在编目里，但必须是合法可配的目标。
func TestLiteModelConstant(t *testing.T) {
	if LiteModel != "wb-lite" {
		t.Fatalf("LiteModel 应为 wb-lite，实际 %q", LiteModel)
	}
}

// 配 wb-lite 作为目标时应能正常通过校验并取到值。
func TestTitleRouteAcceptsLiteModel(t *testing.T) {
	route := &TitleRoute{
		Enabled:  true,
		Domestic: LiteModel,
		Global:   LiteModel,
	}
	if err := ValidateTitleRoute(route); err != nil {
		t.Fatalf("wb-lite 应合法：%v", err)
	}
	if got := route.TargetFor("domestic"); got != LiteModel {
		t.Fatalf("国内站目标错：%q", got)
	}
	if got := route.TargetFor("global"); got != LiteModel {
		t.Fatalf("国际站目标错：%q", got)
	}
}

// 留空仍然表示「沿用请求的模型」，不能被 wb-lite 顶掉。
func TestTitleRouteEmptyStillKeepsRequestModel(t *testing.T) {
	route := &TitleRoute{Enabled: true}
	if got := route.TargetFor("domestic"); got != "" {
		t.Fatalf("留空应返回空串，实际 %q", got)
	}
}
