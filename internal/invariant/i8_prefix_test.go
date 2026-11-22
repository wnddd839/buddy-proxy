package invariant

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 实测抓到的误报（2026-11-22 部署后第一分钟）：
//
//	发出去 "cn:deepseek-v4.1-flash"，上游回显 "deepseek-v4.1-flash"
//	→ I8 报「模型被替换」
//
// `cn:` / `global:` 是客户端显式指定站点的语法前缀，不是模型名的一部分。
// 归一化时必须先剥掉它，否则每一个带站点前缀的请求都会被误报。
func TestI8StripsSitePrefix(t *testing.T) {
	cases := []struct {
		name      string
		requested string
		upstream  string
		wantHit   bool
	}{
		{"cn 前缀", "cn:deepseek-v4.1-flash", "deepseek-v4.1-flash", false},
		{"global 前缀", "global:deepseek-v4.1-flash", "deepseek-v4.1-flash", false},
		{"cn 前缀 + 连字符差异", "cn:hy4flash-free", "hy4-flash-free", false},
		{"cn 前缀 + auto", "cn:auto", "glm-5.3-flash", false},
		{"global 前缀 + auto", "global:auto", "glm-5.3-flash", false},
		{"cn 前缀但真换了模型", "cn:deepseek-v4.1-flash", "glm-5.3-flash", true},
		{"无前缀仍正常判定", "deepseek-v4.1-flash", "glm-5.3-flash", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := decision.Record{
				Kind: decision.KindRequest, At: 1, RequestID: "r-" + c.name,
				Model: c.requested, UpstreamModel: c.upstream, Ok: true,
			}
			hits := 0
			for _, f := range DefaultRegistry().Run(rec, NewState(8, 8)) {
				if f.Invariant == "I8" {
					hits++
				}
			}
			if c.wantHit && hits == 0 {
				t.Fatalf("应命中 I8")
			}
			if !c.wantHit && hits > 0 {
				t.Fatalf("不该命中 I8 —— 站点前缀不是模型名的一部分")
			}
		})
	}
}

// 站点前缀的识别必须只在**开头**，且必须是已知的两个值。
// 模型名里出现冒号（理论上的自定义名）不应被剥。
func TestStripSitePrefixIsConservative(t *testing.T) {
	for _, c := range []struct {
		in, want string
	}{
		{"cn:deepseek-v4.1-flash", "deepseek-v4.1-flash"},
		{"global:glm-5.3-flash", "glm-5.3-flash"},
		{"deepseek-v4.1-flash", "deepseek-v4.1-flash"},
		{"my:custom-model", "my:custom-model"}, // 未知前缀不动
		{"cn:", "cn:"},                         // 只剩前缀时不动（空模型名无意义）
	} {
		if got := stripSitePrefix(c.in); got != c.want {
			t.Errorf("stripSitePrefix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
