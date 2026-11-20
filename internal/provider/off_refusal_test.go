package provider

import "testing"

func TestOffRefusedTable(t *testing.T) {
	if !offRefused("deepseek-v4.1-flash") {
		t.Fatal("deepseek-v4.1-flash 应在拒绝表内")
	}
	// 大小写与空白必须归一
	if !offRefused("  DeepSeek-V4.1-Flash  ") {
		t.Fatal("归一化失败")
	}
	if offRefused("glm-5.3") {
		t.Fatal("glm-5.3 不在表内")
	}
}

// 关键行为：命中拒绝表时，off 必须**整个字段不发**，而不是发出去吃 400。
func TestShouldDropDisableEffort(t *testing.T) {
	cases := []struct {
		model  string
		effort string
		want   bool
	}{
		{"deepseek-v4.1-flash", "off", true},
		{"deepseek-v4.1-flash", "none", true},
		{"deepseek-v4.1-flash", "disabled", true},
		{"deepseek-v4.1-flash", "OFF", true},
		// 非关思考档位一律不删
		{"deepseek-v4.1-flash", "low", false},
		{"deepseek-v4.1-flash", "max", false},
		// 不在拒绝表的模型不动
		{"glm-5.3", "off", false},
		{"kimi-k3-1", "off", false},
		// 空值不动
		{"deepseek-v4.1-flash", "", false},
	}
	for _, tc := range cases {
		if got := shouldDropDisableEffort(tc.model, tc.effort); got != tc.want {
			t.Fatalf("%s + %q = %v，期望 %v", tc.model, tc.effort, got, tc.want)
		}
	}
}

// 端到端：deepseek + off 时 body 里不应出现 reasoning_effort。
func TestApplyReasoningFieldsDropsOffForRefusingModel(t *testing.T) {
	body := map[string]any{}
	ApplyReasoningFields(body, ChatOptions{
		Model:           "deepseek-v4.1-flash",
		ReasoningEffort: "off",
	})
	if v, ok := body["reasoning_effort"]; ok {
		t.Fatalf("拒绝表内的模型不该带 reasoning_effort，实际 %v", v)
	}
}

// 对照：不在拒绝表的模型，off 仍要原样透传（上游能正确处理）。
func TestApplyReasoningFieldsKeepsOffForAcceptingModel(t *testing.T) {
	body := map[string]any{}
	ApplyReasoningFields(body, ChatOptions{
		Model:           "glm-5.3",
		ReasoningEffort: "off",
	})
	if body["reasoning_effort"] != "off" {
		t.Fatalf("glm-5.3 的 off 应原样透传，实际 %v", body["reasoning_effort"])
	}
}

// 正常档位不受影响。
func TestApplyReasoningFieldsUnaffectedForNormalEffort(t *testing.T) {
	body := map[string]any{}
	ApplyReasoningFields(body, ChatOptions{
		Model:           "deepseek-v4.1-flash",
		ReasoningEffort: "max",
	})
	if body["reasoning_effort"] != "max" {
		t.Fatalf("max 应原样写入，实际 %v", body["reasoning_effort"])
	}
}
