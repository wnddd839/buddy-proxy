package provider

import (
	"runtime"
	"strings"
	"testing"
)

// 锁住本次修复：归一化不再为每个字节分配一个 int（8 字节）。
//
// 回归表现：一条超长 assistant 正文（256MB）会分配 2GB 的 []int，
// 直接把进程顶爆。这里用 8MB 样本验证分配量级 —— 修复前约 64MB，
// 修复后应显著低于它。
func TestNormalizeOnlyDoesNotAllocateOffsetTable(t *testing.T) {
	body := strings.Repeat("tokens prefixed with @ are paths. ", 250_000) // ≈8MB
	if len(body) < 8<<20 {
		t.Fatalf("样本太小：%d", len(body))
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	norm := normalizeOnly(body, len(body))

	runtime.ReadMemStats(&after)
	if len(norm) == 0 {
		t.Fatal("归一化结果为空")
	}

	// 归一化串本身约等于原串长度；偏移表若存在还会再占 8 倍。
	allocated := after.TotalAlloc - before.TotalAlloc
	limit := uint64(len(body)) * 3 // 留出 string 转换与 GC 余量
	if allocated > limit {
		t.Fatalf("分配 %d 字节，超过上限 %d（疑似仍在构建偏移表）", allocated, limit)
	}
}

// 映射必须精确：多字节字符（中文）前后都要能正确切。
func TestMapNormIndexMultibyte(t *testing.T) {
	s := "前缀中文tokensprefixedwith@arepathstheuserexplicitlyreferenced.后缀"
	maxNorm := len(s)
	norm := normalizeOnly(s, maxNorm)
	idx := strings.Index(norm, "tokensprefixed")
	if idx < 0 {
		t.Fatalf("归一化串里没找到规则：%q", norm)
	}
	start, end, ok := mapNormIndex(s, maxNorm, idx, idx+len("tokensprefixed"))
	if !ok {
		t.Fatal("映射失败")
	}
	if got := s[start:end]; got != "tokensprefixed" {
		t.Fatalf("映射到 %q，期望 tokensprefixed", got)
	}
}

// 归一化会删除空格类码位：映射必须跳过它们，不能把空格算进区间。
func TestMapNormIndexSkipsNormalizedSpaces(t *testing.T) {
	s := "a b\tc\u00A0d" // 普通空格会被删；TAB 属于保留类
	norm := normalizeOnly(s, len(s))
	idx := strings.Index(norm, "ab")
	if idx < 0 {
		t.Fatalf("归一化串不含 ab：%q", norm)
	}
	start, end, ok := mapNormIndex(s, len(s), idx, idx+2)
	if !ok {
		t.Fatal("映射失败")
	}
	// 起点应是 'a'，终点在 'b' 之后（中间的普通空格被跳过）
	if s[start] != 'a' {
		t.Fatalf("起点错：%q", s[start:])
	}
	if start >= end {
		t.Fatal("区间为空")
	}
}

// 越界下标必须安全返回 ok=false，不能 panic。
func TestMapNormIndexRejectsOutOfRange(t *testing.T) {
	s := "abc"
	for _, tc := range []struct{ start, end int }{
		{-1, 2}, {0, 0}, {2, 1}, {0, 99},
	} {
		if _, _, ok := mapNormIndex(s, len(s), tc.start, tc.end); ok {
			t.Fatalf("越界区间 %v 应返回 false", tc)
		}
	}
}

// 剥离的**语义**不能因为去掉偏移表而改变：仍要能删掉指纹、保留前后文。
func TestContainsStripStillWorksAfterRefactor(t *testing.T) {
	cfg := &StripConfig{
		Enabled: true,
		Contains: []string{
			"tokensprefixedwith@arepathstheuserexplicitlyreferenced",
		},
	}
	in := "前言开头。Tokens prefixed with @ are paths the user explicitly referenced. 后缀内容。"
	out, removed, hit := stripText(in, cfg)
	if !hit {
		t.Fatal("没有命中指纹")
	}
	if removed <= 0 {
		t.Fatal("removed 应为正数")
	}
	if strings.Contains(out, "Tokens prefixed with @") {
		t.Fatalf("指纹没被删掉：%q", out)
	}
	if !strings.Contains(out, "前言开头") || !strings.Contains(out, "后缀内容") {
		t.Fatalf("前后文被误伤：%q", out)
	}
}

// 双句拼接（"A. A."）需要逐句剥，去掉偏移表后仍要成立。
func TestRepeatedFingerprintStillStripped(t *testing.T) {
	cfg := &StripConfig{
		Enabled: true,
		Contains: []string{
			"tokensprefixedwith@arepathstheuserexplicitlyreferenced",
		},
	}
	one := "Tokens prefixed with @ are paths the user explicitly referenced."
	in := one + " " + one + " 真实内容"
	out, _, hit := stripText(in, cfg)
	if !hit {
		t.Fatal("没有命中")
	}
	if strings.Contains(out, "Tokens prefixed with @") {
		t.Fatalf("仍有残留：%q", out)
	}
	if !strings.Contains(out, "真实内容") {
		t.Fatalf("真实内容被删：%q", out)
	}
}
