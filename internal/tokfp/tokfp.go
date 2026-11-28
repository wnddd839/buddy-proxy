package tokfp

import (
	"strings"
	"unicode/utf8"
)

// MaxScanChars 是分类统计的最大扫描量。
//
// 超过它只对前缀分类并标记 Sampled —— 成分**比例**是文本的统计属性，
// 用 320KB 前缀估 1.2MB 全体的误差远小于 I9 的 8% 容差。
//
// 注意：采样只影响**分类**，不影响**长度**。长度用 TotalChars 精确计算。
const MaxScanChars = 320 * 1024

// Composition 是 prompt 文本的字符成分统计。
//
// 必须分类而不是只数总长：不同类别的 token 密度差 2~3 倍
// （ASCII 0.20 / 中文 0.64 / 符号 0.68），混在一起算比例会互相抵消，
// 把真正的分词器差异淹没。
type Composition struct {
	// Chars 是**已分类的**字符数。采样时只覆盖前缀。
	Chars  int
	CJK    int
	Digits int
	ASCII  int
	Other  int
	// Sampled 表示分类只做了前缀。
	Sampled bool
	// TotalChars 是**全文字符数**，精确，不受采样影响。
	//
	// 为什么单独一个字段：采样该只影响「分类」（哪个桶），
	// 不该影响「长度」。长度精确可算 —— ASCII 段按字节数（零成本），
	// 其余一次 RuneCountInString。
	//
	// 用途：Ratio = promptTokens / TotalChars。若用采样后的 Chars，
	// 分母只是前缀，比值会被放大数倍（实测 0.31 的真实值算成 2.11）。
	TotalChars int
}

// Of 统计一组消息的内容成分。
//
// 只统计 content，不统计 role/工具定义 —— 那些在各模型间相同，
// 计入分母只会稀释信号。
func Of(messages []map[string]any) Composition {
	var c Composition
	remaining := MaxScanChars
	for _, m := range messages {
		c.addContent(m["content"], &remaining)
	}
	return c
}

// addContent 处理一条消息的 content（string 或 [{type,text}] 数组）。
func (c *Composition) addContent(v any, remaining *int) {
	switch content := v.(type) {
	case string:
		c.add(content, remaining)
	case []any:
		for _, part := range content {
			m, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if s, ok := m["text"].(string); ok {
				c.add(s, remaining)
			}
		}
	}
	// nil / 数字 / 对象不参与统计：它们不是历史正文。
}

// add 统计一段文本。
//
// 两个独立的目标：
//   - **长度**（TotalChars）：全部文本、精确，不受 remaining 影响
//   - **分类**（Chars/CJK/Digits/ASCII/Other）：只到 remaining 用完为止
//
// 实现上把「数长度」和「分类」分开走，避免早期版本那种把两者混在
// 一个计数器里、导致重复累加的 bug。
func (c *Composition) add(text string, remaining *int) {
	n := len(text)

	// ── 目标一：精确长度 ────────────────────────────────
	// 纯 ASCII 段按字节数（字节数 = 字符数），其余 RuneCountInString。
	asciiEnd := 0
	for asciiEnd < n && text[asciiEnd] < utf8.RuneSelf {
		asciiEnd++
	}
	c.TotalChars += asciiEnd // ASCII 前缀：字节数即字符数
	if asciiEnd < n {
		c.TotalChars += utf8.RuneCountInString(text[asciiEnd:])
	}

	// ── 目标二：分类（受 remaining 约束）────────────────
	if *remaining <= 0 {
		c.Sampled = true
		return
	}

	i := 0
	// 分类 ASCII 前缀
	for i < n && i < *remaining && text[i] < utf8.RuneSelf {
		if b := text[i]; b >= '0' && b <= '9' {
			c.Digits++
		} else {
			c.ASCII++
		}
		i++
	}
	c.Chars += i
	*remaining -= i

	if i >= n {
		return // 整段分类完
	}
	if *remaining <= 0 {
		c.Sampled = true
		return
	}

	// 分类非 ASCII 部分
	consumed := i
	for _, r := range text[i:] {
		if *remaining <= 0 {
			c.Sampled = true
			break
		}
		consumed++
		c.Chars++
		*remaining--
		switch {
		case isCJK(r):
			c.CJK++
		case r >= '0' && r <= '9':
			c.Digits++
		case r < utf8.RuneSelf:
			c.ASCII++
		default:
			c.Other++
		}
	}
	_ = consumed
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0x3040 && r <= 0x30FF) ||
		(r >= 0xAC00 && r <= 0xD7AF)
}

// 分桶名。同一桶内的字符成分相近，比例才有可比性。
const (
	BucketCJK    = "cjk"
	BucketDigits = "digits"
	BucketASCII  = "ascii"
	BucketTiny   = "tiny" // 太短，模板开销占比过高，不参与判定
	BucketNone   = "none" // 无文本
)

// MinCharsForFingerprint 是参与指纹判定的最小字符数。
const MinCharsForFingerprint = 500

// 分桶阈值（占非空字符的百分比）。
const (
	cjkShareForBucket   = 0.30
	digitShareForBucket = 0.20
)

// Bucket 返回该成分所属的桶。
func (c Composition) Bucket() string {
	if c.Chars == 0 {
		return BucketNone
	}
	// 用全文长度判断是否「太短」：采样时 Chars 是前缀，不代表全文。
	chars := c.TotalChars
	if chars <= 0 {
		chars = c.Chars
	}
	if chars < MinCharsForFingerprint {
		return BucketTiny
	}
	if float64(c.CJK)/float64(c.Chars) >= cjkShareForBucket {
		return BucketCJK
	}
	if float64(c.Digits)/float64(c.Chars) >= digitShareForBucket {
		return BucketDigits
	}
	return BucketASCII
}

// Usable 报告这份成分能否参与指纹判定。
func (c Composition) Usable() bool {
	b := c.Bucket()
	return b != BucketTiny && b != BucketNone
}

// Ratio 返回 token 数与**全文字符数**的比值。
//
// 用 TotalChars 而不是 Chars：后者在采样时只是前缀长度。
func (c Composition) Ratio(tokens int) float64 {
	chars := c.TotalChars
	if chars <= 0 {
		chars = c.Chars // 兜底
	}
	if chars <= 0 || tokens <= 0 {
		return 0
	}
	return float64(tokens) / float64(chars)
}

// contentText 把 content 展平成字符串（仅用于需要合并文本的场景）。
func contentText(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var b strings.Builder
		for _, part := range c {
			m, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if s, ok := m["text"].(string); ok {
				b.WriteString(s)
			}
		}
		return b.String()
	default:
		return ""
	}
}
