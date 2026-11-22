package tokfp

import (
	"strings"
	"unicode/utf8"
)

// MaxScanChars 是单次成分统计的最大扫描量。
//
// 超过它只扫前缀并标记 Sampled —— 成分比例是文本的**统计属性**，
// 用 320KB 前缀估 1.2MB 全体的误差远小于 I9 的 8% 容差，
// 而扫描量降到 1/4。
const MaxScanChars = 320 * 1024

// Composition 是 prompt 文本的字符成分统计。
//
// 必须分类而不是只数总长：不同类别的 token 密度差 2~3 倍
// （ASCII 0.20 / 中文 0.64 / 符号 0.68），混在一起算比例会互相抵消，
// 把真正的分词器差异淹没。
type Composition struct {
	Chars  int // 总字符数
	CJK    int // 中日韩文字
	Digits int // 数字
	ASCII  int // 其它 ASCII（字母、空格、标点）
	Other  int // 其它（emoji、全角标点等）
	// Sampled 为 true 表示只在文本前缀上统计（超长 prompt 的采样路径）。
	// 各处比例仍可信，但绝对计数不是全量。
	Sampled bool
}

// Of 统计一组消息的内容成分。
//
// 只统计 content，不统计 role/工具定义 —— 那些在各模型间相同，
// 计入分母只会稀释信号。
//
// ## 性能（2026-11-22 实测，i5-8365U，纯 ASCII 文本）
//
//	42KB   179µs → 121µs（单遍扫描）
//	1.2MB  8.7ms → 2.9ms 后再降到 ~0.9ms（320KB 采样上限）
//
// 单遍扫描的两条要点：
//   - 不再先用 utf8.RuneCountInString 数长度（那是第二次解码）
//   - 纯 ASCII 前缀走字节循环，长度 = 字节数
//
// 采样上限的意义：dsh 的长上下文可达 1.2MB，而成分比例只需要统计意义上准确。
func Of(messages []map[string]any) Composition {
	var c Composition
	remaining := MaxScanChars
	for _, m := range messages {
		if remaining <= 0 {
			c.Sampled = true
			break
		}
		limit := remaining
		used := c.addContent(m["content"], limit)
		remaining -= used
		if used >= limit {
			c.Sampled = true
		}
	}
	return c
}

// addContent 处理一条消息的 content（string 或 [{type,text}] 数组），
// 返回实际扫描的字符数。
func (c *Composition) addContent(v any, limit int) int {
	switch content := v.(type) {
	case string:
		return c.add(content, limit)
	case []any:
		used := 0
		for _, part := range content {
			m, ok := part.(map[string]any)
			if !ok {
				continue
			}
			s, ok := m["text"].(string)
			if !ok {
				continue
			}
			if limit-used <= 0 {
				c.Sampled = true
				break
			}
			used += c.add(s, limit-used)
		}
		return used
	}
	// nil / 数字 / 对象不参与统计：它们不是历史正文。
	return 0
}

// add 统计一段文本的前 limit 个字符：ASCII 前缀按字节走，其余单遍 range。
func (c *Composition) add(text string, limit int) int {
	i := 0
	n := len(text)

	// 纯 ASCII 前缀：字节数即字符数，同时分类数字/其它。
	for i < n && i < limit && text[i] < utf8.RuneSelf {
		if b := text[i]; b >= '0' && b <= '9' {
			c.Digits++
		} else {
			c.ASCII++
		}
		i++
	}
	c.Chars += i

	if i >= n {
		return i // 整段处理完
	}
	if i >= limit {
		c.Sampled = true
		return i
	}

	// 非 ASCII 部分：一次 range 同时数长度 + 分类。
	consumed := i
	for _, r := range text[i:] {
		if consumed >= limit {
			c.Sampled = true
			break
		}
		consumed++
		c.Chars++
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
	return consumed
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
	if c.Chars < MinCharsForFingerprint && !c.Sampled {
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

// Ratio 返回 token 数与**扫描到的**字符数的比值。
//
// 采样路径下这是「前缀的 token/字符比」—— 上游的 prompt_tokens 是全量，
// 所以采样会让 Ratio 偏低（前缀只占全文一部分）。
// 因此采样只在「比例本身」有意义时使用（分桶与趋势），
// 绝对锚定走离线校准样本（固定文本、全量计算）。
func (c Composition) Ratio(tokens int) float64 {
	if c.Chars <= 0 || tokens <= 0 {
		return 0
	}
	return float64(tokens) / float64(c.Chars)
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
