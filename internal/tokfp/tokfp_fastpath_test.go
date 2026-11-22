package tokfp

import (
	"strings"
	"testing"
)

// 快路径（纯 ASCII 前缀）与慢路径（range）必须给出完全一致的分类结果。
//
// 这条是性能优化的护栏：快路径按字节分类、慢路径按 rune 分类，
// 两者只有在语义相同时才能互换。
func TestFastPathEquivalence(t *testing.T) {
	cases := []string{
		"",
		"abc",
		"123",
		"abc123",
		"a b c",
		"{}[]();:=+-*/<>!&|^%$#@~`\\",
		"line1\nline2\r\nline3\t",
		// 混合：从第一个非 ASCII 字节起走慢路径
		"abc中文",
		"中文abc",
		"abc 123 中文，🙂",
		"混合 mixed 数据 42",
		"\u00e9\u00e8\u00ea", // 拉丁重音，属 Other
		"🙂🎉🚀",
		"こんにちは",
		"안녕하세요",
		"a\u00e9b", // 纯 ASCII 段 + 1 个非 ASCII
		"中文",       // 纯非 ASCII
		"🙂",        // 单个 emoji
	}
	for _, text := range cases {
		var fast Composition
		fast.add(text, MaxScanChars)

		// 参考实现：完全逐 rune，不优化
		var want Composition
		for _, r := range text {
			want.Chars++
			switch {
			case isCJK(r):
				want.CJK++
			case r >= '0' && r <= '9':
				want.Digits++
			case r < 0x80:
				want.ASCII++
			default:
				want.Other++
			}
		}

		if fast != want {
			t.Errorf("文本 %q：快路径 %+v，逐 rune %+v", text, fast, want)
		}
	}
}

// 长纯 ASCII 也必须走快路径且结果正确。
func TestFastPathLongASCII(t *testing.T) {
	text := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 100)
	var c Composition
	c.add(text, MaxScanChars)
	if c.CJK != 0 || c.Digits != 0 || c.Other != 0 {
		t.Fatalf("纯英文不该有非 ASCII 分类：%+v", c)
	}
	if c.ASCII != len(text) || c.Chars != len(text) {
		t.Fatalf("长度不对：%+v", c)
	}
}

// 一条消息里混合纯 ASCII 与非 ASCII 段时结果仍正确。
func TestFastPathMixedMessage(t *testing.T) {
	var c Composition
	c.add("abc", MaxScanChars)
	c.add("中文", MaxScanChars)
	c.add("123", MaxScanChars)
	if c.Chars != 8 || c.ASCII != 3 || c.CJK != 2 || c.Digits != 3 {
		t.Fatalf("%+v", c)
	}
}

// 超长文本必须触发采样，且比例仍接近全量。
//
// 采样上限的意义：1.2MB 的长上下文只需统计意义上的比例准确。
func TestSamplingKeepsRatioAccurate(t *testing.T) {
	unit := "这是一段用于测试分词器差异的中文文本，包含常见的双字词与四字成语。"
	text := strings.Repeat(unit, 40000) // 远超 320KB

	var full Composition
	full.add(text, len(text)+1)

	var sampled Composition
	sampled.add(text, MaxScanChars)

	if !sampled.Sampled {
		t.Fatal("超长文本应标记 Sampled")
	}
	if sampled.Chars > MaxScanChars+len(unit) {
		t.Fatalf("采样字符数 %d 超出上限 %d", sampled.Chars, MaxScanChars)
	}
	// 纯中文文本，采样与全量的 CJK 占比应几乎一致
	fullRatio := float64(full.CJK) / float64(full.Chars)
	sampledRatio := float64(sampled.CJK) / float64(sampled.Chars)
	if diff := fullRatio - sampledRatio; diff > 0.001 || diff < -0.001 {
		t.Fatalf("采样占比偏差过大：全量 %.4f vs 采样 %.4f", fullRatio, sampledRatio)
	}
}

// 未超限时不得标记 Sampled。
func TestNoSamplingBelowLimit(t *testing.T) {
	var c Composition
	c.add("short text", MaxScanChars)
	if c.Sampled {
		t.Fatal("短文本不该标记 Sampled")
	}
}
