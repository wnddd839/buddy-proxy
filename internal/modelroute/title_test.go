package modelroute

import "testing"

// wb-lite 是隐藏模型，不在编目里，但必须是合法可配的目标。
func TestLiteModelConstant(t *testing.T) {
	if LiteModel != "wb-lite" {
		t.Fatalf("LiteModel 应为 wb-lite，实际 %q", LiteModel)
	}
}

func TestValidateTitleRoute(t *testing.T) {
	if err := ValidateTitleRoute(nil); err != nil {
		t.Fatalf("nil 应放行：%v", err)
	}
	if err := ValidateTitleRoute(&TitleRoute{Enabled: false}); err != nil {
		t.Fatalf("未启用应放行：%v", err)
	}
	// 启用但两站留空也放行：TargetFor 会回落到 wb-lite
	if err := ValidateTitleRoute(&TitleRoute{Enabled: true}); err != nil {
		t.Fatalf("启用但无显式目标应放行：%v", err)
	}
	if err := ValidateTitleRoute(&TitleRoute{Enabled: true, Domestic: "hy4flash-free"}); err != nil {
		t.Fatalf("只配国内站应放行：%v", err)
	}
}

// 默认目标：启用后未配置的站点一律回落到 wb-lite。
func TestTitleRouteDefaultsToLiteModel(t *testing.T) {
	route := &TitleRoute{Enabled: true}
	if got := route.TargetFor("domestic"); got != LiteModel {
		t.Fatalf("国内站默认目标应为 wb-lite，实际 %q", got)
	}
	if got := route.TargetFor("global"); got != LiteModel {
		t.Fatalf("国际站默认目标应为 wb-lite，实际 %q", got)
	}
}

// 显式配置的值优先于默认。
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

// 只配一个站点：另一个站点回落到 wb-lite（不是「沿用请求模型」）。
func TestTitleRouteSingleSiteOtherFallsBackToLite(t *testing.T) {
	route := &TitleRoute{Enabled: true, Domestic: "hy4flash-free"}
	if got := route.TargetFor("domestic"); got != "hy4flash-free" {
		t.Fatalf("国内站应尊重显式配置：%q", got)
	}
	if got := route.TargetFor("global"); got != LiteModel {
		t.Fatalf("国际站应回落到 wb-lite，实际 %q", got)
	}
}

// 显式配成 wb-lite 与留空等价。
func TestTitleRouteAcceptsLiteModel(t *testing.T) {
	route := &TitleRoute{Enabled: true, Domestic: LiteModel, Global: LiteModel}
	if err := ValidateTitleRoute(route); err != nil {
		t.Fatalf("wb-lite 应合法：%v", err)
	}
	if got := route.TargetFor("domestic"); got != LiteModel {
		t.Fatalf("目标错：%q", got)
	}
}

// 关闭时一律不改写，即使配了目标。
func TestTitleRouteDisabledNeverTargets(t *testing.T) {
	route := &TitleRoute{Enabled: false, Domestic: LiteModel, Global: LiteModel}
	if got := route.TargetFor("domestic"); got != "" {
		t.Fatalf("未启用时不该返回目标：%q", got)
	}
	// nil 安全
	var nilRoute *TitleRoute
	if got := nilRoute.TargetFor("domestic"); got != "" {
		t.Fatalf("nil 应返回空：%q", got)
	}
}

func TestIsSessionTitleRequest(t *testing.T) {
	titleBody := SessionTitlePrefix + ` [{"role":"user","content":"帮我看下这个 bug"}]`

	cases := []struct {
		name string
		msgs []map[string]any
		want bool
	}{
		{"标准标题请求", []map[string]any{{"role": "user", "content": titleBody}}, true},
		{
			"content 是 text 块数组",
			[]map[string]any{{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": titleBody}}}},
			true,
		},
		{"前面有空白", []map[string]any{{"role": "user", "content": "\n\n  " + titleBody}}, true},
		{
			"有 assistant 消息 → 不是标题",
			[]map[string]any{
				{"role": "user", "content": titleBody},
				{"role": "assistant", "content": "标题：修 bug"}},
			false,
		},
		{
			"有 tool 消息 → 不是标题",
			[]map[string]any{
				{"role": "user", "content": titleBody},
				{"role": "tool", "content": "result"}},
			false,
		},
		{
			"两条 user → 不是标题",
			[]map[string]any{
				{"role": "user", "content": titleBody},
				{"role": "user", "content": "再来一句"}},
			false,
		},
		{"普通对话 → 不是标题", []map[string]any{{"role": "user", "content": "帮我写个函数"}}, false},
		{"空消息", nil, false},
		{"只有 system", []map[string]any{{"role": "system", "content": "you are helpful"}}, false},
		{
			"对话里引用了模板文本",
			[]map[string]any{
				{"role": "user", "content": "我在日志里看到 `" + SessionTitlePrefix + "` 这段，它是什么意思？"}},
			false,
		},
	}

	for _, tc := range cases {
		if got := IsSessionTitleRequest(tc.msgs); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}
