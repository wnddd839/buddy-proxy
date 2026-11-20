package modelroute

import "testing"

func TestValidateTitleRoute(t *testing.T) {
	// nil 与未启用都放行
	if err := ValidateTitleRoute(nil); err != nil {
		t.Fatalf("nil 应放行：%v", err)
	}
	if err := ValidateTitleRoute(&TitleRoute{Enabled: false}); err != nil {
		t.Fatalf("未启用应放行：%v", err)
	}
	// 启用但两站都留空也放行：语义 = 两站都沿用请求的模型（不产生改写）
	if err := ValidateTitleRoute(&TitleRoute{Enabled: true}); err != nil {
		t.Fatalf("启用但无目标应放行：%v", err)
	}
	// 只配一个站点 → 通过（另一个站点不改写是合法配置）
	if err := ValidateTitleRoute(&TitleRoute{Enabled: true, Domestic: "hy4flash-free"}); err != nil {
		t.Fatalf("只配国内站应放行：%v", err)
	}
	if err := ValidateTitleRoute(&TitleRoute{Enabled: true, Global: "deepseek-v4.1-flash"}); err != nil {
		t.Fatalf("只配国际站应放行：%v", err)
	}
	// 两站都配 → 通过
	if err := ValidateTitleRoute(&TitleRoute{
		Enabled: true, Domestic: "hy4flash-free", Global: "deepseek-v4.1-flash",
	}); err != nil {
		t.Fatalf("两站都配应放行：%v", err)
	}
}

// 目标按站点取：留空表示该站点不改写。
func TestTitleRouteTargetFor(t *testing.T) {
	r := &TitleRoute{
		Enabled:  true,
		Domestic: "hy4flash-free",
		Global:   "deepseek-v4.1-flash",
	}
	if got := r.TargetFor("domestic"); got != "hy4flash-free" {
		t.Fatalf("domestic 目标错：%q", got)
	}
	if got := r.TargetFor("global"); got != "deepseek-v4.1-flash" {
		t.Fatalf("global 目标错：%q", got)
	}
	// 站点别名与大小写
	if got := r.TargetFor("  GLOBAL "); got != "deepseek-v4.1-flash" {
		t.Fatalf("站点归一化失败：%q", got)
	}
	// 未启用时一律不改写
	r.Enabled = false
	if got := r.TargetFor("domestic"); got != "" {
		t.Fatalf("未启用时不该返回目标，实际 %q", got)
	}
	// nil 安全
	var nilRoute *TitleRoute
	if got := nilRoute.TargetFor("domestic"); got != "" {
		t.Fatalf("nil 应返回空，实际 %q", got)
	}
	// 留空的站点不改写
	partial := &TitleRoute{Enabled: true, Domestic: "hy4flash-free"}
	if got := partial.TargetFor("global"); got != "" {
		t.Fatalf("未配置的站点应返回空，实际 %q", got)
	}
}

func TestIsSessionTitleRequest(t *testing.T) {
	titleBody := SessionTitlePrefix + ` [{"role":"user","content":"帮我看下这个 bug"}]`

	cases := []struct {
		name string
		msgs []map[string]any
		want bool
	}{
		{
			name: "标准标题请求",
			msgs: []map[string]any{{"role": "user", "content": titleBody}},
			want: true,
		},
		{
			name: "content 是 text 块数组",
			msgs: []map[string]any{
				{"role": "user", "content": []any{
					map[string]any{"type": "text", "text": titleBody},
				}},
			},
			want: true,
		},
		{
			name: "前面有空白",
			msgs: []map[string]any{{"role": "user", "content": "\n\n  " + titleBody}},
			want: true,
		},
		{
			name: "有 assistant 消息 → 不是标题",
			msgs: []map[string]any{
				{"role": "user", "content": titleBody},
				{"role": "assistant", "content": "标题：修 bug"},
			},
			want: false,
		},
		{
			name: "有 tool 消息 → 不是标题",
			msgs: []map[string]any{
				{"role": "user", "content": titleBody},
				{"role": "tool", "content": "result"},
			},
			want: false,
		},
		{
			name: "两条 user → 不是标题",
			msgs: []map[string]any{
				{"role": "user", "content": titleBody},
				{"role": "user", "content": "再来一句"},
			},
			want: false,
		},
		{
			name: "普通对话 → 不是标题",
			msgs: []map[string]any{{"role": "user", "content": "帮我写个函数"}},
			want: false,
		},
		{
			name: "空消息",
			msgs: nil,
			want: false,
		},
		{
			name: "只有 system",
			msgs: []map[string]any{{"role": "system", "content": "you are helpful"}},
			want: false,
		},
		{
			// 引用模板的正常对话不能被误判：模板必须出现在开头。
			name: "对话里引用了模板文本",
			msgs: []map[string]any{
				{"role": "user", "content": "我在日志里看到 `" + SessionTitlePrefix + "` 这段，它是什么意思？"},
			},
			want: false,
		},
	}

	for _, tc := range cases {
		if got := IsSessionTitleRequest(tc.msgs); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}
