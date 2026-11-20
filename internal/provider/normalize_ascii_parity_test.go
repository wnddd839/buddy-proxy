package provider

import (
	"strings"
	"testing"
)

// normalizeOnly 的 ASCII 快路径必须与逐 rune 参考实现产生完全相同的输出。
//
// 这是本次改动的唯一正确性约束：快路径只允许「更快」，不允许「不一样」。
// 归一化串一旦分叉，指纹匹配与 mapNormIndex 的偏移映射都会错位 ——
// 表现是剥离切错位置或漏剥，都是难复现的线上事故。
func TestNormalizeOnlyASCIIParity(t *testing.T) {
	cases := []string{
		"", " ", "  ", "Hello World", "HELLO", "hElLo WoRlD",
		"a\tb\nc\rd", "a\x00b", "a\x7fb",
		"第 1 轮 Hello World", "ＡＢＣ", "İstanbul", "straße",
		"tokens prefixed with @ are paths",
		"MiXeD CaSe TeXt 123 !@#$%^&*()",
		"\u3000全角空格\u00A0不换行",
		"ａｂｃＡＢＣ",
		"emoji 😀 mixed ASCII text",
		strings.Repeat("AbC dEf 中文 ", 40),
	}
	for _, s := range cases {
		got := normalizeOnly(s, len(s)+16)
		want := normalizeOnlyRuneRef(s, len(s)+16)
		if got != want {
			t.Errorf("ASCII 快路径与 rune 路径不一致:\n  in   = %q\n  fast = %q\n  ref  = %q", s, got, want)
		}
	}
}

// 边界：maxNormBytes 截断行为也必须一致。
func TestNormalizeOnlyASCIIParityBounded(t *testing.T) {
	s := "Hello World 中文 ABCDEFG hijklmn"
	for _, limit := range []int{0, 1, 2, 5, 11, 12, 20, len(s)} {
		got := normalizeOnly(s, limit)
		want := normalizeOnlyRuneRef(s, limit)
		if got != want {
			t.Errorf("limit=%d 不一致: fast=%q ref=%q", limit, got, want)
		}
	}
}

// normalizeOnlyRuneRef 是改造前的逐 rune 实现，作为参考基线。
func normalizeOnlyRuneRef(s string, maxNormBytes int) string {
	if s == "" || maxNormBytes <= 0 {
		return ""
	}
	norm := make([]byte, 0, min(len(s), maxNormBytes+8))
	for i := 0; i < len(s) && len(norm) < maxNormBytes; {
		r, size := decodeRune(s[i:])
		if normalizedSpace(r) {
			i += size
			continue
		}
		norm = append(norm, toLowerRune(r)...)
		i += size
	}
	return string(norm)
}

// 剥离行为端到端：命中必须真删，未命中必须零改动。
func TestStripContainsBehaviorUnchanged(t *testing.T) {
	rules := ContainsForSite("domestic")
	fp := "Tokens prefixed with @ are paths the user explicitly referenced. Relative paths resolve from the workspace root"

	hit := "前言 " + fp + " 后记"
	out, removed, changed := stripBlockedContains(hit, rules)
	if !changed || removed == 0 {
		t.Fatalf("命中却没剥离: changed=%v removed=%d", changed, removed)
	}
	if strings.Contains(normalizeOnly(out, len(out)+8), rules[0]) {
		t.Errorf("剥离后仍含指纹: %q", out)
	}
	if !strings.Contains(out, "前言") || !strings.Contains(out, "后记") {
		t.Errorf("剥离误删了周围正文: %q", out)
	}

	clean := "这是一段完全没有指纹的正常正文，讲的是代码风格。"
	out2, removed2, changed2 := stripBlockedContains(clean, rules)
	if changed2 || removed2 != 0 || out2 != clean {
		t.Errorf("未命中却改动了内容: changed=%v removed=%d out=%q", changed2, removed2, out2)
	}
}

// 带空格/大小写变形的指纹也必须能剥掉。
func TestStripContainsCaseAndSpaceVariants(t *testing.T) {
	rules := ContainsForSite("domestic")
	variants := []string{
		"tokens prefixed with @ are paths the user explicitly referenced. relative paths resolve from the workspace root",
		"TOKENS PREFIXED WITH @ ARE PATHS THE USER EXPLICITLY REFERENCED. RELATIVE PATHS RESOLVE FROM THE WORKSPACE ROOT",
		"tokensprefixedwith@arepathstheuserexplicitlyreferenced.relativepathsresolvefromtheworkspaceroot",
	}
	for _, v := range variants {
		s := "头部 " + v + " 尾部"
		out, _, changed := stripBlockedContains(s, rules)
		if !changed {
			t.Errorf("变形指纹未被剥离: %q", v)
			continue
		}
		if strings.Contains(normalizeOnly(out, len(out)+8), rules[0]) {
			t.Errorf("剥离后仍含指纹: in=%q out=%q", v, out)
		}
		if !strings.Contains(out, "头部") || !strings.Contains(out, "尾部") {
			t.Errorf("剥离误删上下文: %q", out)
		}
	}
}
