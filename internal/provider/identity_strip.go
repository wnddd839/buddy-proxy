package provider

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

// decodeRune / toLowerRune 是 utf8/unicode 的薄封装，单独提出来是为了
// 让 scanNormalize 的意图更清楚：对原串按 rune 解码、对归一化按小写比较。
func decodeRune(s string) (rune, int) {
	return utf8.DecodeRuneInString(s)
}

func toLowerRune(r rune) string {
	return string(unicode.ToLower(r))
}

// 本文件实现「上游 11128 身份句剥离」。
//
// 背景
// ----
// 上游 CodeBuddy 对 system 内容做一次「精确整句」黑名单扫描，命中即回
// 400 code 11128 "Illegal API invocation from an unapproved channel"。
// 这不是关键词过滤，也不是长度/指纹头问题 —— 2026-11-16 用控制变量实弹
// 反推得到如下判定模型（每个结论都有对应探测）：
//
//	对每条 system/developer 消息：
//	  归一化 = 删除 16 个空格类码位 + 转小写
//	  命中   = 归一化后以某条黑名单串开头（HasPrefix）
//
// 实测要点：
//   - 只有「开头」才拦：前加任意字符 → 放行；夹在中间 → 放行
//   - 尾部有容差：句末加 . ! ? 空格 仍拦
//   - 大小写、连续空格、NBSP/全角空格/ZWSP/BOM 等都被归一化掉
//   - TAB / LF / CR / VT / FF / U+1680 / U+2028 / U+2029 不参与归一化
//     （含这些字符的变体上游本来就不拦，故漏处理无风险）
//   - 只扫 system，不扫 user
//   - 任意一条 system 命中都拦，不限首条
//   - 需要循环剥离：双句拼接时剥一句仍拦，剥两句才放行
//
// 站点差异（实测）
// ------------
//   - Claude Code 整句：domestic 与 global 都拦
//   - DeepSeek Harness 身份句：仅 domestic 拦，global 放行
//
// 为什么不通用识别
// --------------
// 实测 "You are Cline, an AI coding assistant."、"You are Cursor, ..."、
// "You are Codex, ..."、"You are GitHub Copilot, ..."、"You are DeepSeek,
// an AI assistant."、"You are an AI agent powered by X Harness." 全部放行。
// 黑名单只有精确句，没有「类别」特征 —— 通用正则只会误删正常人设。

// blockedIdentityPrefixes 是各站点的身份句黑名单（归一化形态：已删空格 + 小写）。
//
// 维护方式：上游新增黑名单句时，在此追加对应站点的归一化串。
// 归一化串的生成方法：把原句转小写、删掉全部空格类码位。
var blockedIdentityPrefixes = map[string][]string{
	"domestic": {
		"youareclaudecode,anthropic'sofficialcliforclaude",
		"youareanaiagentpoweredbydeepseekharness",
	},
	"global": {
		"youareclaudecode,anthropic'sofficialcliforclaude",
	},
}

// normalizedSpace 判断某个 rune 是否属于「上游归一化时会删除的空格类码位」。
//
// 这份集合是实弹枚举出来的（见 TestScanNormalizeRemovesSpaces）：
// 删除 16 个，保留 8 个。特别注意 TAB/LF/CR 属于「保留」，
// 与 Go 的 unicode.IsSpace 不同 —— 不要用 IsSpace 代替。
func normalizedSpace(r rune) bool {
	switch r {
	case '\u0020', // SPACE
		'\u00A0', // NO-BREAK SPACE
		'\u2000', // EN QUAD
		'\u2002', // EN SPACE
		'\u2003', // EM SPACE
		'\u2007', // FIGURE SPACE
		'\u2009', // THIN SPACE
		'\u200A', // HAIR SPACE
		'\u200B', // ZERO WIDTH SPACE
		'\u200C', // ZERO WIDTH NON-JOINER
		'\u200D', // ZERO WIDTH JOINER
		'\u202F', // NARROW NO-BREAK SPACE
		'\u205F', // MEDIUM MATHEMATICAL SPACE
		'\u3000', // IDEOGRAPHIC SPACE
		'\uFEFF', // ZERO WIDTH NO-BREAK SPACE / BOM
		'\u00AD': // SOFT HYPHEN
		return true
	}
	return false
}

// scanNormalize 返回删除空格类码位并转小写后的串，
// 以及「归一化串第 i 个 byte → 原串 byte 偏移」的映射。
//
// 映射是必需的：原串可能含多字节字符（中文 AGENTS.md），
// 只有映射回原串的真实 byte 偏移才能精确切掉前缀。
//
// 注意：这里按 byte 逐个处理而不是 rune 逐个，因为归一化串只需要
// 支持 ASCII 黑名单比较；但小写化用 unicode 语义，故对非 ASCII 走
// rune 解码以正确计算其原串 byte 宽度。
func scanNormalize(s string) (string, []int) {
	norm := make([]byte, 0, len(s))
	offsets := make([]int, 0, len(s))
	for i := 0; i < len(s); {
		r, size := decodeRune(s[i:])
		if normalizedSpace(r) {
			i += size
			continue
		}
		lower := toLowerRune(r)
		// 归一化串里每个输出 byte 都指向原串该 rune 的起始偏移。
		// 小写化后 byte 数可能与原 rune 不同（例如某些 Unicode 字符），
		// 但黑名单只含 ASCII，实际比较时不会走到那种分支。
		for k := 0; k < len(lower); k++ {
			norm = append(norm, lower[k])
			offsets = append(offsets, i)
		}
		i += size
	}
	return string(norm), offsets
}

// stripBlockedIdentity 循环剥离 system 内容开头命中的身份句。
//
// 返回剥离后的内容、删除的字节数、是否发生过剥离。
// 未命中时原样返回入参（零拷贝路径：不重建字符串）。
//
// 循环是必需的：实测 "A. A."（同句拼接两次）剥掉一句后仍命中 11128。
func stripBlockedIdentity(s string, rules []string) (string, int, bool) {
	if s == "" || len(rules) == 0 {
		return s, 0, false
	}
	original := s
	hit := false
	for {
		rest, cut := cutBlockedPrefix(s, rules)
		if !cut {
			break
		}
		s = rest
		hit = true
		if s == "" {
			break
		}
	}
	if !hit {
		return original, 0, false
	}
	return s, len(original) - len(s), true
}

// cutBlockedPrefix 若 s 归一化后以某条规则开头，返回「切掉该句后」的余下内容。
//
// 切法：按 offsets 找到该句在原串中的结束 byte 偏移，然后跳过紧跟的
// 一个句末标点（. ! ?）与前后空白，余下部分原样返回。
func cutBlockedPrefix(s string, rules []string) (string, bool) {
	norm, offsets := scanNormalize(s)
	if norm == "" || len(offsets) != len(norm) {
		return s, false
	}
	for _, rule := range rules {
		if rule == "" || !strings.HasPrefix(norm, rule) {
			continue
		}
		// 规则在归一化串里的最后一个 byte 下标 → 原串中该字符的起始偏移
		last := offsets[len(rule)-1]
		// 原串中该字符的结束偏移：取下一个 rune 的起点（或串尾）
		_, size := decodeRune(s[last:])
		end := last + size
		// 吃掉紧跟的一个句末标点（实测句末 . ! ? 都不影响命中）
		if end < len(s) && isSentenceTerminator(s[end]) {
			end++
		}
		// 跳过后续空白（含换行），但保留其后的全部内容
		for end < len(s) {
			r, sz := decodeRune(s[end:])
			if !isTrimmableSpace(r) {
				break
			}
			end += sz
		}
		return s[end:], true
	}
	return s, false
}

// isSentenceTerminator 判断是否为剥离时要一并吃掉的句末标点。
func isSentenceTerminator(b byte) bool {
	return b == '.' || b == '!' || b == '?'
}

// isTrimmableSpace 判断剥离后要跳过的空白。
//
// 这里用 unicode.IsSpace 语义（含 TAB/LF/CR），因为这是「删掉句子后
// 清理分隔符」，与上游归一化的字符集是两件不同的事。
func isTrimmableSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f',
		'\u0085', '\u00A0', '\u1680', '\u2000', '\u2001', '\u2002',
		'\u2003', '\u2004', '\u2005', '\u2006', '\u2007', '\u2008',
		'\u2009', '\u200A', '\u2028', '\u2029', '\u202F', '\u205F',
		'\u3000', '\uFEFF':
		return true
	}
	return false
}

// RulesForSite 返回该站点要用的剥离规则。
//
// 设计取舍（重要）：这里永远返回该站点的**全量黑名单**，不做「按客户端
// 只剥它那一条」的减法。原因是客户端识别依赖 User-Agent / 专属头，而
// 这些信号可以缺失或被伪造；一旦按客户端做减法，识别失败就等于漏剥，
// 请求仍会被上游 11128 拒收。客户端类型只用于事件观测，不参与剥离决策。
//
// 未知站点回落 global（与 config.NormalizeSite 的默认口径一致），
// 且保证绝不返回空规则 —— 空规则等于静默关闭剥离。
func RulesForSite(site string) []string {
	if rules, ok := blockedIdentityPrefixes[config.NormalizeSite(site)]; ok && len(rules) > 0 {
		return rules
	}
	return blockedIdentityPrefixes["global"]
}

// StripConfig 描述一次请求的剥离配置。
type StripConfig struct {
	Enabled bool
	Rules   []string
}

// StripReport 回填一次请求实际发生的剥离情况，供网关记事件。
// 指针由调用方传入；为 nil 时不记录。
type StripReport struct {
	Stripped bool
	Bytes    int
}

func (r *StripReport) add(removed int) {
	if r == nil || removed <= 0 {
		return
	}
	r.Stripped = true
	r.Bytes += removed
}

// stripContent 对一条消息的 content 做剥离，返回新 content 与是否改动。
//
// 支持两种形态：
//   - string：直接剥离
//   - []any ：逐 part 处理，只动 type 为 "text"（或未标注 type）的 part；
//     剥空则把该 part 的 text 置空（保留 part 结构，不改变数组长度）
//
// 其他类型（nil / 数字 / 未知结构）一律不碰 —— 保守处理，
// 宁可漏剥也不破坏客户端结构。
func stripContent(content any, cfg *StripConfig, report *StripReport) (any, bool) {
	if cfg == nil || !cfg.Enabled || len(cfg.Rules) == 0 {
		return content, false
	}
	switch v := content.(type) {
	case string:
		out, removed, hit := stripBlockedIdentity(v, cfg.Rules)
		if !hit {
			return content, false
		}
		report.add(removed)
		return out, true
	case []any:
		changed := false
		for _, part := range v {
			pm, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if typ, ok := pm["type"].(string); ok && typ != "" && typ != "text" {
				continue // 图片等非文本 part 不动
			}
			text, ok := pm["text"].(string)
			if !ok {
				continue
			}
			out, removed, hit := stripBlockedIdentity(text, cfg.Rules)
			if !hit {
				continue
			}
			pm["text"] = out
			report.add(removed)
			changed = true
		}
		if !changed {
			return content, false
		}
		return v, true
	default:
		return content, false
	}
}
