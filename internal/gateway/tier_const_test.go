package gateway

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

// 档位字符串必须在 decision 与 provider 两侧钉在一起。
//
// ## 为什么需要这条测试（评审 Minor 2）
//
// I12 的判据只吃 decision.Record，所以它不能 import provider（那会让
// 「只吃数据」的包反向依赖具体实现）。代价是它在代码里硬编码了
// "low" / "high" / "max" 三个字符串 —— provider 侧一旦改名（比如
// EffortMax 的值从 "max" 变成 "maximum"），I12 会**静默失明**：
// 所有请求的 tier 都不再匹配，judged 恒为 false，断言永远 0 命中，
// 而面板会显示「一切正常」。这类失明没有任何日志能提示。
//
// 这条测试放在 gateway：它同时 import decision 与 provider，
// 两侧常量一旦分叉就会在这里炸。
func TestReasoningTierConstantsMatchProvider(t *testing.T) {
	cases := []struct {
		decisionConst string
		providerConst string
		name          string
	}{
		{decision.ReasoningTierLow, provider.EffortLow, "low"},
		{decision.ReasoningTierHigh, provider.EffortHigh, "high"},
		{decision.ReasoningTierMax, provider.EffortMax, "max"},
	}
	for _, c := range cases {
		if c.decisionConst != c.providerConst {
			t.Fatalf("%s 档：decision=%q 与 provider=%q 不一致 —— I12 会静默失明",
				c.name, c.decisionConst, c.providerConst)
		}
	}
}

// 三个常量的值必须是 I12 判据里写的那三个字面串。
//
// 这条钉的是「日志里实际落盘的字符串」：老数据（部署前）里没有 tier，
// 新数据里是这三个值。改常量必须同时想清楚历史数据怎么迁移。
func TestReasoningTierConstantsAreWireValues(t *testing.T) {
	if decision.ReasoningTierLow != "low" ||
		decision.ReasoningTierHigh != "high" ||
		decision.ReasoningTierMax != "max" {
		t.Fatalf("档位常量必须是落盘的线值 low/high/max，实际 %q/%q/%q",
			decision.ReasoningTierLow, decision.ReasoningTierHigh, decision.ReasoningTierMax)
	}
}

// 空串必须永远表示「不可判」，不能被任一常量覆盖。
//
// I12 靠空串把「客户端没要求思考 / 部署前老记录」排除在外。
// 若哪天有人把某个常量定义成空串，这部分记录会突然变成可判。
func TestReasoningTierConstantsAreNonEmpty(t *testing.T) {
	for _, v := range []string{
		decision.ReasoningTierLow, decision.ReasoningTierHigh, decision.ReasoningTierMax,
	} {
		if v == "" {
			t.Fatal("档位常量不能为空串 —— 空串的语义是「不可判」")
		}
	}
}
