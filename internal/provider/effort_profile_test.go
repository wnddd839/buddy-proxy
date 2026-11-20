package provider

import "testing"

func TestProfileFromReasoningPluralForm(t *testing.T) {
	p := ProfileFromReasoning(map[string]any{
		"supportedEfforts":   []any{"low", "high", "max"},
		"defaultEffort":      "high",
		"canDisableThinking": true,
	})
	if !p.Known {
		t.Fatal("Known 应为 true")
	}
	if len(p.Supported) != 3 || p.Supported[0] != "low" || p.Supported[2] != "max" {
		t.Fatalf("Supported 解析错：%v", p.Supported)
	}
	if p.Default != "high" {
		t.Fatalf("Default=%q", p.Default)
	}
	if !p.CanDisable {
		t.Fatal("CanDisable 应为 true")
	}
}

// 单数形态（只有 effort）必须折叠成全阶梯，而不是「只支持一档」。
// 依据：实测这些模型对每档返回不同的 reasoning_content，且不发时不思考。
func TestProfileFromReasoningSingularFormFoldsToLadder(t *testing.T) {
	p := ProfileFromReasoning(map[string]any{"effort": "medium"})
	if len(p.Supported) != len(effortLadder) {
		t.Fatalf("单数形态应折叠成全阶梯，实际 %v", p.Supported)
	}
	if p.Default != "medium" {
		t.Fatalf("Default 应保留声明的值，实际 %q", p.Default)
	}
}

func TestProfileFromReasoningUnknown(t *testing.T) {
	if p := ProfileFromReasoning(nil); p.Known {
		t.Fatal("nil 应为 unknown")
	}
	if p := ProfileFromReasoning(map[string]any{}); p.Known {
		t.Fatal("空对象应为 unknown")
	}
	if p := ProfileFromReasoning(map[string]any{"summary": "auto"}); p.Known {
		t.Fatal("只有 summary 时应为 unknown")
	}
}

// 档位顺序必须按强弱排，不能按上游数组顺序 —— 上游可能乱序给出。
func TestProfileSupportedOrderIsStable(t *testing.T) {
	p := ProfileFromReasoning(map[string]any{
		"supportedEfforts": []any{"max", "low", "high"},
	})
	want := []string{"low", "high", "max"}
	for i, w := range want {
		if p.Supported[i] != w {
			t.Fatalf("顺序错：%v", p.Supported)
		}
	}
}

func TestMiddleEffortNeverWeakest(t *testing.T) {
	cases := []struct {
		supported []string
		want      string
	}{
		{[]string{"low", "high"}, "high"},
		{[]string{"low", "high", "max"}, "high"},
		{[]string{"high", "xhigh"}, "xhigh"},
		{[]string{"low"}, "low"},
	}
	for _, tc := range cases {
		p := EffortProfile{Known: true, Supported: tc.supported}
		if got := p.MiddleEffort(); got != tc.want {
			t.Fatalf("%v 的 MiddleEffort=%q，期望 %q", tc.supported, got, tc.want)
		}
	}
}

func TestCanonicalEffortForProfile(t *testing.T) {
	// deepseek-v4.1-flash：low/high/max
	ds := ProfileFromReasoning(map[string]any{
		"supportedEfforts": []any{"low", "high", "max"},
		"defaultEffort":    "high",
	})

	cases := []struct {
		name    string
		raw     string
		profile EffortProfile
		want    string
	}{
		{"直接支持", "max", ds, "max"},
		{"别名映射后支持", "ultra", ds, "max"},
		{"minimal 映射到 low", "minimal", ds, "low"},
		// xhigh 不在 low/high/max 里，别名目标是 high，high 支持 → high
		{"别名目标支持", "xhigh", ds, "high"},
	}
	for _, tc := range cases {
		got, _ := canonicalEffortForProfile(tc.raw, tc.profile)
		if got != tc.want {
			t.Fatalf("%s: %q → %q，期望 %q", tc.name, tc.raw, got, tc.want)
		}
	}
}

// 关键保证：模型不支持的档位必须回落，不能原样透传。
// 上游对未知档位静默接受但完全不思考，透传等于悄悄关掉思考。
func TestCanonicalEffortNeverPassesUnsupported(t *testing.T) {
	glm52 := ProfileFromReasoning(map[string]any{
		"supportedEfforts": []any{"high", "xhigh"},
		"defaultEffort":    "high",
	})
	// 客户端要 low，但 glm-5.2 只有 high/xhigh
	got, sub := canonicalEffortForProfile("low", glm52)
	if got == "low" {
		t.Fatal("不支持的档位被原样透传了")
	}
	if !sub {
		t.Fatal("应标记为已替换")
	}
	if got != "high" && got != "xhigh" {
		t.Fatalf("回落到了奇怪的值：%q", got)
	}
}

// 未知值仍然回落，不静默关思考。
func TestCanonicalEffortForProfileUnknownValue(t *testing.T) {
	ds := ProfileFromReasoning(map[string]any{
		"supportedEfforts": []any{"low", "high", "max"},
		"defaultEffort":    "high",
	})
	got, sub := canonicalEffortForProfile("bogus-xyz", ds)
	if got != "high" {
		t.Fatalf("未知值应回落 high，实际 %q", got)
	}
	if !sub {
		t.Fatal("应标记为已替换")
	}
}

// 无档位信息时退回全局三档逻辑，行为不能变。
func TestCanonicalEffortForProfileFallsBackWhenUnknownModel(t *testing.T) {
	var unknown EffortProfile
	if got, _ := canonicalEffortForProfile("ultra", unknown); got != "max" {
		t.Fatalf("unknown 模型应走全局收敛，实际 %q", got)
	}
	if got, _ := canonicalEffortForProfile("bogus", unknown); got != "high" {
		t.Fatalf("unknown 模型的未知值应回落 high，实际 %q", got)
	}
}

// 空串表示「客户端没指定」，必须原样返回空，不能替换成 default ——
// 否则「不传档位 = 不思考」这个用户可见的语义会被悄悄改掉。
func TestCanonicalEffortForProfileEmptyStaysEmpty(t *testing.T) {
	ds := ProfileFromReasoning(map[string]any{
		"supportedEfforts": []any{"low", "high", "max"},
		"defaultEffort":    "high",
	})
	if got, _ := canonicalEffortForProfile("", ds); got != "" {
		t.Fatalf("空档位应保持为空，实际 %q", got)
	}
}
