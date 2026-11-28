package gateway

import (
	"os"
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/tokfp"
)

// 标定请求必须绕过网关自身的消息变换。
//
// 实测事故（2026-11-22）：线上 echo 开关开着（SYSTEM_ECHO_USER=1），
// 于是 s_u 结构里 system 被复制进 user 并包裹，上游返回 19 而直连只有 7。
// 多出的 12 个 token 被算进「模板开销」，导致公式校验误判为不符。
//
// 这条测试用源码检查而不是跑请求：标定会真的打上游、烧 credit。
func TestIdentityProbeBypassesEcho(t *testing.T) {
	raw, err := os.ReadFile("tokidentity.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "chatOpts.EchoSystemToUser = false") {
		t.Fatal("标定请求必须显式关闭 EchoSystemToUser —— 否则量的是「网关+上游」而非上游本身")
	}
}

// 模板公式的四种结构必须与实测吻合。
//
// 期望值来自 2026-11-22 直连实测（绕过网关变换后）：
//
//	1u    官方 1×2 上游 5  → overhead 4
//	2u    官方 1×2 上游 7  → overhead 5
//	u_a_u 官方 1×3 上游 11 → overhead 8
//	s_u   官方 1×2 上游 7  → overhead 5
func TestTemplateFormulaMatchesDirectMeasurements(t *testing.T) {
	want := map[string]int{
		"1u（单条 user）":        5,
		"2u（两条 user）":        7,
		"u_a_u（一轮问答）":        11,
		"s_u（system + user）": 7,
	}
	if len(tokfp.TemplateCases) != len(want) {
		t.Fatalf("TemplateCases 数量 %d，期望 %d", len(tokfp.TemplateCases), len(want))
	}
	for _, tc := range tokfp.TemplateCases {
		w, ok := want[tc.Name]
		if !ok {
			t.Fatalf("缺少实测样本：%s", tc.Name)
		}
		official := 0
		for _, m := range tc.Messages {
			if m.Content != "" {
				official += tokfp.TemplateProbeOfficialTokens
			}
		}
		if got := official + tc.ExpectedOverhead; got != w {
			t.Errorf("%s: 公式预测 %d，实测 %d", tc.Name, got, w)
		}
	}
}
