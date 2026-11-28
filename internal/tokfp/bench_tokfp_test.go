package tokfp

import (
	"strings"
	"testing"
)

// 基准测试必须分别覆盖「纯 ASCII」与「含 CJK」两种形态。
//
// ## 为什么之前的基准测不出东西
//
// 早期版本只用了中文样本，而中文必然走 rune 慢路径 ——
// ASCII 快路径根本没被执行，测出来的数字自然没有改善。
// 这是基准设计错误，不是优化无效。
func asciiPrompt(bytes int) []map[string]any {
	unit := "The quick brown fox jumps over the lazy dog. "
	body := strings.Repeat(unit, bytes/len(unit)+1)[:bytes]
	return []map[string]any{
		{"role": "system", "content": body[:bytes/2]},
		{"role": "user", "content": body[bytes/2:]},
	}
}

func cjkPrompt(chars int) []map[string]any {
	unit := "这是一段用于测试分词器差异的中文文本，包含常见的双字词与四字成语。"
	body := strings.Repeat(unit, chars/len(unit)+1)
	if len(body) > chars {
		body = body[:chars]
	}
	return []map[string]any{
		{"role": "system", "content": body[:chars/2]},
		{"role": "user", "content": body[chars/2:]},
	}
}

func BenchmarkOfASCII(b *testing.B) {
	for _, size := range []int{42000, 1200000} {
		p := asciiPrompt(size)
		name := "42KB"
		if size > 100000 {
			name = "1.2MB"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				Of(p)
			}
		})
	}
}

func BenchmarkOfCJK(b *testing.B) {
	for _, size := range []int{42000, 1200000} {
		p := cjkPrompt(size)
		name := "42KB"
		if size > 100000 {
			name = "1.2MB"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				Of(p)
			}
		})
	}
}
