package tokfp

import (
	"strings"
	"testing"
)

// 校准常量自检：重建出来的文本必须与标定时逐字节一致。
//
// 守的是「生成脚本被改动后，常量与文本不再对应」这一类事故。
// 不需要 tokenizer —— 只校验结构（单元长度 + 重复次数 + 官方计数）。
//
// ⚠️ 长度用 **字节**：Go 的 len(string) 是字节数，中文每字 3 字节。
// 下面的期望值由 Python 的字符数换算而来（中文 3300 字符 = 9900 字节）。
func TestCalibrationSelfCheck(t *testing.T) {
	want := []struct {
		name  string
		unit  int // 字节
		rep   int
		bytes int // 字节
		tok   int
	}{
		// 中文：33 字符 × 3 字节 = 99；3300 字符 = 9900 字节
		{"中文", 99, 100, 9900, 2100},
		// 英文全 ASCII：45 字符 = 45 字节；4499 字符 = 4499 字节
		{"英文", 45, 100, 4499, 1000},
		// 数字全 ASCII：55 字符 = 55 字节；5499 字符 = 5499 字节
		{"数字", 55, 100, 5499, 2499},
	}
	if len(CalibrationSamples) != len(want) {
		t.Fatalf("样本数 %d，want %d", len(CalibrationSamples), len(want))
	}
	for i, w := range want {
		s := CalibrationSamples[i]
		if s.Name != w.name {
			t.Errorf("第 %d 个样本名 %q，want %q", i, s.Name, w.name)
		}
		if len(s.Unit) != w.unit {
			t.Errorf("%s 单元字节长度 %d，want %d", w.name, len(s.Unit), w.unit)
		}
		if s.Rep != w.rep {
			t.Errorf("%s 重复次数 %d，want %d", w.name, s.Rep, w.rep)
		}
		text := s.Text()
		if len(text) != w.bytes {
			t.Errorf("%s 重建文本字节长度 %d，want %d", w.name, len(text), w.bytes)
		}
		if s.OfficialTokens != w.tok {
			t.Errorf("%s 官方计数 %d，want %d", w.name, s.OfficialTokens, w.tok)
		}
	}
}

// 模板开销公式必须与实测的四个结构全部吻合。
//
// 这是身份鉴定的基础：公式错了，判据就错。
func TestFormattedOverheadMatchesMeasured(t *testing.T) {
	for _, c := range TemplateCases {
		got := FormattedOverhead(c.Messages)
		if got != c.ExpectedOverhead {
			t.Errorf("%s: 公式算出 %d，实测期望 %d", c.Name, got, c.ExpectedOverhead)
		}
	}
}

// 补测公式在更多结构上的表现（期望值来自 2026-11-22 实测）。
func TestFormattedOverheadExtended(t *testing.T) {
	cases := []struct {
		name string
		msgs []TemplateMessage
		want int
	}{
		{"3u", []TemplateMessage{
			{Role: "user"}, {Role: "user"}, {Role: "user"},
		}, 6},
		{"u_a", []TemplateMessage{
			{Role: "user"}, {Role: "assistant"},
		}, 7},
		{"u_a_u_a", []TemplateMessage{
			{Role: "user"}, {Role: "assistant"}, {Role: "user"}, {Role: "assistant"},
		}, 11},
		{"u_a_a_u", []TemplateMessage{
			{Role: "user"}, {Role: "assistant"}, {Role: "assistant"}, {Role: "user"},
		}, 9},
		{"u_a_a_a_u", []TemplateMessage{
			{Role: "user"}, {Role: "assistant"}, {Role: "assistant"},
			{Role: "assistant"}, {Role: "user"},
		}, 10},
		{"u_a_u_a_u_a_u", []TemplateMessage{
			{Role: "user"}, {Role: "assistant"}, {Role: "user"}, {Role: "assistant"},
			{Role: "user"}, {Role: "assistant"}, {Role: "user"},
		}, 16},
		{"s_u_a_u", []TemplateMessage{
			{Role: "system"}, {Role: "user"}, {Role: "assistant"}, {Role: "user"},
		}, 9},
	}
	for _, c := range cases {
		if got := FormattedOverhead(c.msgs); got != c.want {
			t.Errorf("%s: 公式 %d，实测 %d", c.name, got, c.want)
		}
	}
}

// 身份鉴定：三项一致且等于模板开销 ⇒ 官方 DeepSeek 词表。
func TestAnalyzeIdentityDeepSeek(t *testing.T) {
	exp := FormattedOverhead([]TemplateMessage{{Role: "user"}})
	observed := make([]int, len(CalibrationSamples))
	for i, s := range CalibrationSamples {
		observed[i] = s.OfficialTokens + exp
	}
	rep := AnalyzeIdentity("deepseek-v4.1-flash", observed)
	if rep.Vocabulary != VerdictPass {
		t.Fatalf("应判为官方词表：%+v", rep)
	}
	if !rep.TemplateConsistent || rep.TemplateDelta != exp {
		t.Fatalf("模板差值应为 %d：%+v", exp, rep)
	}
}

// 各样本差值不一致 ⇒ 不是官方词表。
// 用真实数据形态：glm 中文 +112、英文 +12、数字 +812。
func TestAnalyzeIdentityDifferentVocab(t *testing.T) {
	observed := []int{
		CalibrationSamples[0].OfficialTokens + 112,
		CalibrationSamples[1].OfficialTokens + 12,
		CalibrationSamples[2].OfficialTokens + 812,
	}
	rep := AnalyzeIdentity("glm-5.3-flash", observed)
	if rep.Vocabulary != VerdictFail {
		t.Fatalf("应判为非官方词表：%+v", rep)
	}
	for _, s := range rep.Samples {
		if s.Verdict != VerdictFail {
			t.Errorf("%s 也应判 Fail：%+v", s.Name, s)
		}
	}
	if rep.TemplateConsistent {
		t.Fatal("差值不一致时不该认为模板一致")
	}
	if rep.VocabularyLabel() != "非 DeepSeek 词表" {
		t.Errorf("标签不对：%q", rep.VocabularyLabel())
	}
}

// 差值一致但不等 ⇒ 词表可能相同、模板不同（不要误判成「换了模型」）。
func TestAnalyzeIdentityTemplateMismatch(t *testing.T) {
	observed := make([]int, len(CalibrationSamples))
	for i, s := range CalibrationSamples {
		observed[i] = s.OfficialTokens + 9 // 一致，但不是 4
	}
	rep := AnalyzeIdentity("m", observed)
	if rep.Vocabulary != VerdictFail {
		t.Fatalf("应判 Fail（模板不符）：%+v", rep)
	}
	if !rep.TemplateConsistent {
		t.Fatal("差值一致时 TemplateConsistent 应为 true")
	}
	if !strings.Contains(rep.Note, "聊天模板不同") {
		t.Errorf("说明应指出是模板问题而非词表：%q", rep.Note)
	}
}

// 未观测到 prompt_tokens 时全部为 unknown，不能谎报「通过」。
func TestAnalyzeIdentityNoObservation(t *testing.T) {
	rep := AnalyzeIdentity("m", []int{0, 0, 0})
	if rep.Vocabulary != VerdictUnknown {
		t.Fatalf("无观测应判 Unknown：%+v", rep)
	}
	if rep.VocabularyLabel() != "无法判定" {
		t.Errorf("标签应为「无法判定」，实际 %q", rep.VocabularyLabel())
	}
}

// 权重永远「无法验证」—— 这条必须稳定，不能被改成「通过」。
//
// 词表相同不能证明权重相同：蒸馏/微调模型可以继承词表。
// 上游不返回 logprobs（实测三个模型都没有），所以无从验证。
func TestWeightLabelAlwaysUnverifiable(t *testing.T) {
	rep := AnalyzeIdentity("m", nil)
	if rep.WeightLabel() != "无法验证（上游不提供 logprobs）" {
		t.Fatalf("权重标签必须稳定：%q", rep.WeightLabel())
	}
}
