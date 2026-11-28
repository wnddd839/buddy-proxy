package provider

import "testing"

func TestCanonicalReasoningEffort(t *testing.T) {
	cases := []struct {
		in        string
		wantWire  string
		wantSubst bool
	}{
		// 6 个官方取值收敛到 3 个真实档位。
		{"minimal", EffortLow, false},
		{"low", EffortLow, false},
		{"medium", EffortHigh, false},
		{"high", EffortHigh, false},
		{"xhigh", EffortHigh, false},
		{"max", EffortMax, false},
		// ultra 实测与 max 无差异，收敛到 max。
		{"ultra", EffortMax, false},
		// 大小写与空白无关。
		{"  HIGH  ", EffortHigh, false},
		{"Ultra", EffortMax, false},
		// 「关掉思考」类关键词原样透传，交给上游判断。
		{"none", "none", false},
		{"off", "off", false},
		{"disabled", "disabled", false},
		// 空值 ⇒ 不写，保持「客户端没传」的语义。
		{"", "", false},
		{"   ", "", false},
		// 未知值不能原样透传：上游会静默关掉思考。
		{"bogus-xyz", UnknownEffortFallback, true},
		{"highest", UnknownEffortFallback, true},
		{"maxx", UnknownEffortFallback, true},
	}
	for _, c := range cases {
		wire, subst := CanonicalReasoningEffort(c.in)
		if wire != c.wantWire || subst != c.wantSubst {
			t.Fatalf("CanonicalReasoningEffort(%q) = (%q,%v), want (%q,%v)",
				c.in, wire, subst, c.wantWire, c.wantSubst)
		}
	}
}

func TestUnknownEffortDoesNotSilentlyDisableThinking(t *testing.T) {
	// 这条锁的是本次实机发现：未知档位值原样透传时，上游返回
	// reasoning_tokens=0 且正文 0 字（3/3 复现），等于悄悄关掉思考。
	// 修复后必须落到一个真实档位，且 ReasoningRequested 要能看见它。
	body := map[string]any{"model": "auto"}
	ApplyReasoningFields(body, ChatOptions{ReasoningEffort: "bogus-xyz"})

	got, _ := body["reasoning_effort"].(string)
	if got != UnknownEffortFallback {
		t.Fatalf("reasoning_effort=%q, want fallback %q", got, UnknownEffortFallback)
	}
	if !ReasoningRequested(ChatOptions{ReasoningEffort: "bogus-xyz"}) {
		t.Fatal("unknown effort must still count as 'reasoning requested', otherwise zero-think samples bypass monitoring")
	}
}

func TestUltraRoutesToMax(t *testing.T) {
	// ultra 与 max 实测无差异（各 10 次交错中位 5885 vs 5632，为正 4/10），
	// 收敛到 max 以免上游多一种未定义行为。
	body := map[string]any{"model": "auto"}
	ApplyReasoningFields(body, ChatOptions{ReasoningEffort: "ultra"})
	if body["reasoning_effort"] != EffortMax {
		t.Fatalf("ultra should route to max, got %v", body["reasoning_effort"])
	}
	if !ReasoningRequested(ChatOptions{ReasoningEffort: "ultra"}) {
		t.Fatal("ultra must count as reasoning requested")
	}
}

func TestObjectEffortAlsoCanonicalised(t *testing.T) {
	// 对象形式抬升出来的 effort 也必须走同一条收敛，
	// 否则「只发对象」的客户端仍会把未知值透传给上游。
	body := map[string]any{"model": "auto"}
	ApplyReasoningFields(body, ChatOptions{
		Reasoning: map[string]any{"effort": "ultra"},
	})
	if body["reasoning_effort"] != EffortMax {
		t.Fatalf("object ultra should lift as max, got %v", body["reasoning_effort"])
	}
}
