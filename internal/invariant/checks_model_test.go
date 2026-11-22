package invariant

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// I8 的判据来自 2026-11-22 的真实探测结果：
//
//	deepseek-v4.1-flash → deepseek-v4.1-flash   一致
//	hy4flash-free       → hy4-flash-free        差一个连字符（不算替换）
//	wb-lite             → wb-lite               一致
//	auto                → glm-5.3-flash         上游解析（豁免）
//
// 直接相等比较会把 hy4flash-free 误报成偷换，所以必须归一化。
func TestI8ModelSubstitution(t *testing.T) {
	cases := []struct {
		name      string
		requested string
		routed    string
		upstream  string
		wantHit   bool
	}{
		{"完全一致", "deepseek-v4.1-flash", "", "deepseek-v4.1-flash", false},
		{"只差连字符", "hy4flash-free", "", "hy4-flash-free", false},
		{"大小写差异", "GLM-5.3-Flash", "", "glm-5.3-flash", false},
		{"auto 由上游选", "auto", "", "glm-5.3-flash", false},
		{"auto-xxx 同族", "auto-fast", "", "glm-5.3-flash", false},
		{"上游未回显", "deepseek-v4.1-flash", "", "", false},
		{"真被换成别的模型", "deepseek-v4.1-flash", "", "glm-5.3-flash", true},
		{"路由改写后与回显一致", "alias", "deepseek-v4.1-flash", "deepseek-v4.1-flash", false},
		{"路由改写后上游又换了", "alias", "deepseek-v4.1-flash", "glm-5.3-flash", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := decision.Record{
				Kind: decision.KindRequest, At: 1, RequestID: "r-" + c.name,
				Model: c.requested, RoutedModel: c.routed,
				UpstreamModel: c.upstream, Ok: true,
			}
			got := DefaultRegistry().Run(rec, NewState(8, 8))
			hits := 0
			for _, f := range got {
				if f.Invariant == "I8" {
					hits++
				}
			}
			if hits != 1 && c.wantHit {
				t.Fatalf("应命中 I8：%+v", got)
			}
			if hits > 0 && !c.wantHit {
				t.Fatalf("不该命中 I8：%+v", got)
			}
		})
	}
}

// I8b 只关心 auto 一族被解析成了什么，且只出 info。
func TestI8bAutoResolution(t *testing.T) {
	rec := decision.Record{
		Kind: decision.KindRequest, At: 1, RequestID: "r1",
		Model: "auto", UpstreamModel: "glm-5.3-flash", Ok: true,
	}
	got := DefaultRegistry().Run(rec, NewState(8, 8))
	var info *decision.Finding
	for i := range got {
		if got[i].Invariant == "I8b" {
			info = &got[i]
		}
	}
	if info == nil {
		t.Fatalf("auto 应出 I8b：%+v", got)
	}
	if info.Severity != SeverityInfo {
		t.Fatalf("I8b 必须是 info：%+v", info)
	}

	// 非 auto 不该出 I8b（对照在 I8）。
	rec2 := decision.Record{
		Kind: decision.KindRequest, At: 1, RequestID: "r2",
		Model: "deepseek-v4.1-flash", UpstreamModel: "deepseek-v4.1-flash", Ok: true,
	}
	for _, f := range DefaultRegistry().Run(rec2, NewState(8, 8)) {
		if f.Invariant == "I8b" {
			t.Fatalf("非 auto 不该出 I8b：%+v", f)
		}
	}
}

// 失败/取消的请求不参与（上游可能压根没回显）。
func TestI8SkipsFailedAndCanceled(t *testing.T) {
	for _, rec := range []decision.Record{
		{Kind: decision.KindRequest, At: 1, Model: "m", UpstreamModel: "other", Ok: false},
		{Kind: decision.KindRequest, At: 1, Model: "m", UpstreamModel: "other", Ok: true, Canceled: true},
	} {
		for _, f := range DefaultRegistry().Run(rec, NewState(8, 8)) {
			if f.Invariant == "I8" {
				t.Fatalf("失败/取消不该命中 I8：%+v", f)
			}
		}
	}
}
