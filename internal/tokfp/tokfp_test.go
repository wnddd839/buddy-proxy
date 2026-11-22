package tokfp

import "testing"

// 成分统计必须把字符正确分类 —— 分类错了，比例就失去意义。
func TestCompositionClassifies(t *testing.T) {
	c := Of([]map[string]any{
		{"role": "user", "content": "abc 123 中文，🙂"},
	})
	// "abc 123 中文，🙂" = 3+1+3+1+2+1+1 = 12 个字符
	if c.Chars != 12 {
		t.Fatalf("Chars = %d, want 12", c.Chars)
	}
	// abc(3) + 空格(1) + 123 后的空格(1) = 5 ASCII；123 = 3 数字；中文=2 CJK；
	// 全角逗号与 emoji = 2 other
	if c.ASCII != 5 {
		t.Errorf("ASCII = %d, want 5", c.ASCII)
	}
	if c.Digits != 3 {
		t.Errorf("Digits = %d, want 3", c.Digits)
	}
	if c.CJK != 2 {
		t.Errorf("CJK = %d, want 2", c.CJK)
	}
	if c.Other != 2 {
		t.Errorf("Other = %d, want 2", c.Other)
	}
}

// content 为数组形态也要计入。
func TestCompositionHandlesContentArray(t *testing.T) {
	c := Of([]map[string]any{{
		"role": "user",
		"content": []any{
			map[string]any{"type": "text", "text": "hello"},
			map[string]any{"type": "text", "text": "世界"},
		},
	}})
	if c.Chars != 7 || c.CJK != 2 {
		t.Fatalf("%+v", c)
	}
}

// 分桶：用实测样本验证阈值。
//
// 实测（2026-11-22）：
//
//	zh   5280 字符 → cjk 桶（比例 0.637~0.669）
//	nums 6600 字符 → digits 桶（比例 0.455~0.602）
//	code 6750 字符 → ascii 桶（比例 0.307~0.322）
func TestBucketFromRealSamples(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		repeat int
		want   string
	}{
		// 实测中文样本：纯中文 + 标点
		{"中文长文", "这是一段用于测试分词器差异的中文文本，包含常见的双字词与四字成语。", 160, BucketCJK},
		// 实测数字样本：纯数字
		{"数字密集", "1234567890 9876543210 5555555555 1111111111 9999999999\n", 120, BucketDigits},
		// 实测代码样本：英文 + 少量符号
		{"代码", "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tx := 1234567\n\tfmt.Println(x)\n}\n\n", 90, BucketASCII},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var sb []byte
			for i := 0; i < c.repeat; i++ {
				sb = append(sb, c.text...)
			}
			comp := Of([]map[string]any{{"role": "user", "content": string(sb)}})
			if got := comp.Bucket(); got != c.want {
				t.Fatalf("Bucket = %q, want %q（成分 %+v）", got, c.want, comp)
			}
			if !comp.Usable() {
				t.Fatalf("实测样本必须可用")
			}
		})
	}
}

// 短 prompt 必须被排除：模板开销占比过高，会把常数差异误读成分词器差异。
func TestShortPromptIsNotUsable(t *testing.T) {
	c := Of([]map[string]any{{"role": "user", "content": "say ok"}})
	if c.Bucket() != BucketTiny {
		t.Fatalf("短 prompt 应进 tiny 桶，实际 %q（%+v）", c.Bucket(), c)
	}
	if c.Usable() {
		t.Fatal("tiny 桶不可用于判定")
	}
}

// 空内容不可用。
func TestEmptyIsNotUsable(t *testing.T) {
	c := Of(nil)
	if c.Bucket() != BucketNone || c.Usable() {
		t.Fatalf("%+v", c)
	}
}

// Ratio 的边界：0 或负值都返回 0，不产生 NaN/Inf。
func TestRatioIsSafe(t *testing.T) {
	c := Composition{Chars: 100}
	if r := c.Ratio(0); r != 0 {
		t.Errorf("tokens=0 → %v", r)
	}
	if r := (Composition{}).Ratio(50); r != 0 {
		t.Errorf("chars=0 → %v", r)
	}
	if got := c.Ratio(64); got != 0.64 {
		t.Errorf("Ratio = %v, want 0.64", got)
	}
}
