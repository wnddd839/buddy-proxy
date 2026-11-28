package server

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

func TestEffortTier(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// 三个真实档位。
		{"low", provider.EffortLow},
		{"high", provider.EffortHigh},
		{"max", provider.EffortMax},
		// 别名收敛到真实档位 —— 这样按 tier 聚合吞吐才有样本量。
		{"minimal", provider.EffortLow},
		{"medium", provider.EffortHigh},
		{"xhigh", provider.EffortHigh},
		{"ultra", provider.EffortMax},
		{"ULTRA", provider.EffortMax},
		// 未知值：修复后会被当成 high 真的去思考，所以 tier 记 high。
		{"bogus-xyz", provider.EffortHigh},
		// 没传 / 明确关闭：不归入任何档位。
		{"", ""},
		{"  ", ""},
		{"none", ""},
		{"off", ""},
		{"disabled", ""},
	}
	for _, c := range cases {
		if got := effortTier(c.in); got != c.want {
			t.Fatalf("effortTier(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEffortTierMatchesWhatIsSentUpstream(t *testing.T) {
	// 锁死一致性：tier 必须等于真正写进上游 body 的值，
	// 否则「日志说 high、实际按 max 跑」这种偏差没人能发现。
	for _, raw := range []string{"minimal", "low", "medium", "high", "xhigh", "max", "ultra", "bogus-xyz"} {
		body := map[string]any{"model": "auto"}
		provider.ApplyReasoningFields(body, provider.ChatOptions{ReasoningEffort: raw})
		sent, _ := body["reasoning_effort"].(string)
		if got := effortTier(raw); got != sent {
			t.Fatalf("%q: tier=%q but sent=%q", raw, got, sent)
		}
	}
}
