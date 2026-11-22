// Package tokfp 用「token 数 / 字符数」的比例做模型分词器指纹。
//
// ## 为什么需要它
//
// 上游可能在中途把请求悄悄路由到别的模型。回显的 model 字段（I8）只能
// 覆盖「它老实说了」的情况；分词器比例是**独立于声明的物理证据**：
// 不同模型对同一段文本切出的 token 数不同，这个差异改不了。
//
// ## 实测依据（2026-11-22，真实请求，同账号同 prompt）
//
//	模型                 zh(5280字)   code(6750字)   nums(6600字)
//	deepseek-v4.1-flash     0.6371       0.3073         0.4552
//	glm-5.3-flash           0.6689       0.3217         0.6018
//	hy4flash / hy4std       0.6100       0.3098         0.4577
//	wb-lite                 0.6371       0.3073         0.4552   ← 与 deepseek 完全恒等
//
// 结论：
//   - **数字密集**的文本最敏感（glm 比 deepseek 高 32%），足以独立判别
//   - 中文相差 4%~6%、代码相差 5%：不足以单独判别，只能配合基线趋势
//   - 英文 delta 四个模型完全一致（448）⇒ 英文分词器相同，不是有效信号
//
// 所以本包只做「算清成分 + 分桶」，判定留给断言层用快/慢双基线做趋势检测。
package tokfp

import "strings"

// Composition 是 prompt 文本的字符成分统计。
//
// 必须分类而不是只数总长：不同类别的 token 密度差 2~3 倍
// （ASCII 0.22 / 中文 0.64 / 数字 0.46~0.60），混在一起算比例会互相抵消，
// 把真正的分词器差异淹没。
type Composition struct {
	Chars  int // 总字符数
	CJK    int // 中日韩文字
	Digits int // 数字
	ASCII  int // 其它 ASCII（字母、空格、标点）
	Other  int // 其它（emoji、全角标点等）
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
//
// 取 500 的理由：chat template 有固定的 overhead（实测 6~23 token），
// 短 prompt 里这部分占比过高，会把「模板开销差异」误读成「分词器差异」。
// 500 字符时 overhead 占比 < 5%，低于我们关心的差异量级。
const MinCharsForFingerprint = 500

// 分桶阈值（占非空字符的百分比）。
//
// digits 阈值取 20%：实测数字密集样本里 glm 与 deepseek 差 32%，
// 但混合少量数字时这个差异会被稀释，所以只有真正数字密集才算。
const (
	cjkShareForBucket   = 0.30
	digitShareForBucket = 0.20
)

// Of 统计一组消息的内容成分。
//
// 只统计 content，不统计 role/工具定义 —— 那些在各模型间相同，
// 计入分母只会稀释信号。
func Of(messages []map[string]any) Composition {
	var c Composition
	for _, m := range messages {
		c.add(contentText(m["content"]))
	}
	return c
}

// add 把一段文本计入统计。
func (c *Composition) add(text string) {
	for _, r := range text {
		c.Chars++
		switch {
		case isCJK(r):
			c.CJK++
		case r >= '0' && r <= '9':
			c.Digits++
		case r < 0x80:
			c.ASCII++
		default:
			c.Other++
		}
	}
}

// contentText 把 content 展平成字符串（支持 string 与 [{type,text}] 数组）。
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

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0x3040 && r <= 0x30FF) ||
		(r >= 0xAC00 && r <= 0xD7AF)
}

// Bucket 返回该成分所属的桶。
func (c Composition) Bucket() string {
	if c.Chars == 0 {
		return BucketNone
	}
	if c.Chars < MinCharsForFingerprint {
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

// Ratio 返回 token 数与字符数的比值（字符数为 0 时返回 0）。
func (c Composition) Ratio(tokens int) float64 {
	if c.Chars <= 0 || tokens <= 0 {
		return 0
	}
	return float64(tokens) / float64(c.Chars)
}
