package tokfp

import (
	"strings"
	"testing"
)

// 快路径与慢路径必须给出完全一致的分类结果（只比分类字段）。
func TestFastPathEquivalence(t *testing.T) {
	cases := []string{
		"", "abc", "123", "abc123", "a b c",
		"{}[]();:=+-*/<>!&|^%$#@~`\\",
		"line1\nline2\r\nline3\t",
		"abc中文", "中文abc", "abc 123 中文，🙂",
		"混合 mixed 数据 42",
		"\u00e9\u00e8\u00ea", "🙂🎉🚀", "こんにちは", "안녕하세요", "a\u00e9b",
		"中文", "🙂",
	}
	for _, text := range cases {
		var fast Composition
		rem := MaxScanChars
		fast.add(text, &rem)

		var want compositionCategories
		for _, r := range text {
			want.chars++
			switch {
			case isCJK(r):
				want.cjk++
			case r >= '0' && r <= '9':
				want.digits++
			case r < 0x80:
				want.ascii++
			default:
				want.other++
			}
		}

		if fast.Chars != want.chars || fast.CJK != want.cjk ||
			fast.Digits != want.digits || fast.ASCII != want.ascii ||
			fast.Other != want.other {
			t.Errorf("文本 %q：快 %+v，慢 %+v", text, fast, want)
		}
		// 短文本不采样，TotalChars 应等于 Chars
		if !fast.Sampled && fast.TotalChars != fast.Chars {
			t.Errorf("文本 %q：TotalChars %d != Chars %d", text, fast.TotalChars, fast.Chars)
		}
	}
}

type compositionCategories struct {
	chars, cjk, digits, ascii, other int
}

func TestFastPathLongASCII(t *testing.T) {
	text := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 100)
	var c Composition
	rem := MaxScanChars
	c.add(text, &rem)
	if c.CJK != 0 || c.Digits != 0 || c.Other != 0 {
		t.Fatalf("纯英文不该有非 ASCII 分类：%+v", c)
	}
	if c.ASCII != len(text) || c.Chars != len(text) {
		t.Fatalf("长度不对：%+v", c)
	}
	if c.TotalChars != len(text) {
		t.Fatalf("TotalChars = %d, want %d", c.TotalChars, len(text))
	}
}

func TestFastPathMixedMessage(t *testing.T) {
	var c Composition
	rem := MaxScanChars
	c.add("abc", &rem)
	c.add("中文", &rem)
	c.add("123", &rem)
	if c.Chars != 8 || c.ASCII != 3 || c.CJK != 2 || c.Digits != 3 {
		t.Fatalf("%+v", c)
	}
}

// 超长文本：采样只影响分类，长度必须精确。
func TestSamplingKeepsRatioAccurate(t *testing.T) {
	unit := "这是一段用于测试分词器差异的中文文本，包含常见的双字词与四字成语。"
	text := strings.Repeat(unit, 40000) // 远超 320KB

	var full Composition
	big := len(text) + 1
	full.add(text, &big)

	var sampled Composition
	rem := MaxScanChars
	sampled.add(text, &rem)

	if !sampled.Sampled {
		t.Fatal("超长文本应标记 Sampled")
	}
	if sampled.Chars > MaxScanChars+len(unit) {
		t.Fatalf("分类字符数 %d 超出上限 %d", sampled.Chars, MaxScanChars)
	}
	// 关键：长度精确，不受采样影响
	if sampled.TotalChars != full.Chars {
		t.Fatalf("采样后 TotalChars %d != 全量 %d", sampled.TotalChars, full.Chars)
	}
	// 分类占比仍接近全量
	fullRatio := float64(full.CJK) / float64(full.Chars)
	sampledRatio := float64(sampled.CJK) / float64(sampled.Chars)
	if diff := fullRatio - sampledRatio; diff > 0.001 || diff < -0.001 {
		t.Fatalf("采样占比偏差过大：全量 %.4f vs 采样 %.4f", fullRatio, sampledRatio)
	}
}

func TestNoSamplingBelowLimit(t *testing.T) {
	var c Composition
	rem := MaxScanChars
	c.add("short text", &rem)
	if c.Sampled {
		t.Fatal("短文本不该标记 Sampled")
	}
	if c.TotalChars != c.Chars {
		t.Fatalf("TotalChars %d != Chars %d", c.TotalChars, c.Chars)
	}
}

// Ratio 必须用全文长度：采样时不能被放大。
//
// 实测形态（2026-11-22 线上）：1.2MB 长上下文、采样上限 320KB 时，
// promptTokens=691677，真实 ascii 比例约 0.31；
// 旧实现用前缀长度算出 2.11（放大 7 倍），基线因此被污染。
func TestRatioUsesTotalCharsWhenSampled(t *testing.T) {
	// 构造超过采样上限的纯 ASCII 文本
	unit := "The quick brown fox jumps over the lazy dog. "
	text := strings.Repeat(unit, 20000) // ~880KB
	if len(text) <= MaxScanChars {
		t.Fatalf("文本只有 %d 字节，不足以触发采样（上限 %d）", len(text), MaxScanChars)
	}

	var c Composition
	rem := MaxScanChars
	c.add(text, &rem)

	if !c.Sampled {
		t.Fatalf("应触发采样：Chars=%d TotalChars=%d", c.Chars, c.TotalChars)
	}
	if c.Chars >= c.TotalChars {
		t.Fatalf("采样时 Chars(%d) 应小于 TotalChars(%d)", c.Chars, c.TotalChars)
	}

	tokens := c.TotalChars / 4 // 假设 4 字符/token
	if r := c.Ratio(tokens); r < 0.20 || r > 0.30 {
		t.Fatalf("Ratio = %.4f，应接近 0.25", r)
	}
	// 对照：用 Chars 会算出明显放大的值（放大幅度 = TotalChars/Chars）
	fake := float64(tokens) / float64(c.Chars)
	real := c.Ratio(tokens)
	if fake <= real*2 {
		t.Fatalf("对照：用 Chars 应放大至少 2 倍，实际 fake=%.4f real=%.4f", fake, real)
	}
}
