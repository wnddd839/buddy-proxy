package provider

import (
	"maps"
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

// blockedContainsRules 是「任意位置命中」的指纹黑名单（归一化形态）。
//
// 与 blockedIdentityPrefixes 的区别：身份句只在消息**开头**才拦，而这类
// 段落指纹出现在 system / assistant 的任意位置都会触发 11128。
// 2026-11-17 实弹反推：归一化串前后贴任意字符仍被拦；删掉其中的 "@" 或把
// "workspace root" 改成 "workspace roox" 即放行 —— 说明是归一化后的子串匹配。
//
// 已知条目来自 dsh（DeepSeek Harness）自带 file-reference 段的开头：
//
//	Tokens prefixed with @ are paths the user explicitly referenced.
//	Relative paths resolve from the workspace root
//
// 该段由 @deepseek-ai/dsh-file-reference 的 FILE_REFERENCE_PROMPT 常量提供，
// 经 dsh-file-reference-local 注册为 system section "context:file-reference"，
// 每个 dsh web 实例都会带，与第三方插件无关。
var blockedContainsRules = map[string][]string{
	"domestic": {
		"tokensprefixedwith@arepathstheuserexplicitlyreferenced.relativepathsresolvefromtheworkspaceroot",
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
	return scanNormalizeBounded(s, len(s))
}

// scanNormalizeBounded 只扫描判定前缀所需的最少归一化字节（上限 maxNormBytes），
// 避免在超长 system 或大量重复身份句上产生 O(N^2) 全文扫描与大块内存分配。
func scanNormalizeBounded(s string, maxNormBytes int) (string, []int) {
	if s == "" || maxNormBytes <= 0 {
		return "", nil
	}
	initCap := min(len(s), maxNormBytes+8)
	norm := make([]byte, 0, initCap)
	offsets := make([]int, 0, initCap)
	for i := 0; i < len(s) && len(norm) < maxNormBytes; {
		r, size := decodeRune(s[i:])
		if normalizedSpace(r) {
			i += size
			continue
		}
		lower := toLowerRune(r)
		for k := 0; k < len(lower); k++ {
			norm = append(norm, lower[k])
			offsets = append(offsets, i)
		}
		i += size
	}
	return string(norm), offsets
}

// normalizeOnly 只产出归一化串，不构建偏移映射。
//
// 单独提供它的理由见 mapNormIndex：偏移表是 []int，8 字节/归一化字节，
// 在超长正文上会把内存放大 8 倍。任何只需要「找命中位置」的场景都该用它。
func normalizeOnly(s string, maxNormBytes int) string {
	if s == "" || maxNormBytes <= 0 {
		return ""
	}
	initCap := min(len(s), maxNormBytes+8)
	norm := make([]byte, 0, initCap)
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

// mapNormIndex 把「归一化串的下标区间 [start, end)」映射回原串的 byte 区间。
//
// 只在确认命中后调用：它重新遍历一遍原串，用与 normalizeOnly 完全相同的
// 归一化规则计数，因此不需要预先保存整张偏移表。无额外分配。
//
// 返回 ok=false 表示下标越界（调用方应当跳过这条规则）或归一化串为空。
func mapNormIndex(s string, maxNormBytes, start, end int) (int, int, bool) {
	if s == "" || start < 0 || end <= start || maxNormBytes <= 0 {
		return 0, 0, false
	}
	origStart, origEnd := -1, -1
	normPos := 0
	for i := 0; i < len(s) && normPos < maxNormBytes; {
		r, size := decodeRune(s[i:])
		if normalizedSpace(r) {
			i += size
			continue
		}
		width := len(toLowerRune(r))
		if origStart < 0 && normPos >= start {
			origStart = i
		}
		// end 是开区间：归一化位置达到 end 时，本 rune 已经出界
		if normPos >= end {
			break
		}
		normPos += width
		if normPos >= end {
			origEnd = i + size
			break
		}
		i += size
	}
	if origStart < 0 || origEnd <= origStart {
		return 0, 0, false
	}
	return origStart, origEnd, true
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

// stripBlockedContains 删除内容中**任意位置**命中的指纹子串。
//
// 与 stripBlockedIdentity 的分工：
//   - 身份句 -> 只在开头命中（HasPrefix），由 stripBlockedIdentity 处理；
//   - 段落指纹 -> 任意位置命中（Contains），由本函数处理。
//
// 循环删除直到不再命中：同一段文字可能出现多次，且删掉一处的拼接结果
// 可能又凑出一个新的命中（实测 "A. A." 的双句拼接需要逐句剥）。
//
// 只删命中的归一化区间，不引入任何替换文本，前后文原样保留。
//
// 每轮只删一处、删完重扫，是为了处理「删掉中间一段后，前后文拼出一个新命中」
// 的情况（前缀剥离的 "A. A." 也是同一类）。轮数上限防病态输入把 CPU 拖死：
// 正常 system 至多一两处，256 轮远大于真实需求。
func stripBlockedContains(s string, rules []string) (string, int, bool) {
	if s == "" || len(rules) == 0 {
		return s, 0, false
	}
	original := s
	for pass := 0; pass < maxContainsStripPasses; pass++ {
		// 只产出归一化串（1 字节/字符），不再同时建 []int 偏移表（8 字节/字符）。
		// 偏移只在命中后才用 mapNormIndex 现算 —— 未命中时完全不产生映射开销。
		norm := normalizeOnly(s, len(s))
		if norm == "" {
			break
		}
		cut := false
		for _, rule := range rules {
			if rule == "" {
				continue
			}
			idx := strings.Index(norm, rule)
			if idx < 0 {
				continue
			}
			start, end, ok := mapNormIndex(s, len(s), idx, idx+len(rule))
			if !ok {
				continue
			}
			s = s[:start] + s[end:]
			cut = true
			break
		}
		if !cut {
			break
		}
	}
	if s == original {
		return original, 0, false
	}
	return s, len(original) - len(s), true
}

// maxContainsStripPasses 限制 contains 剥离的重扫轮数（见 stripBlockedContains）。
const maxContainsStripPasses = 256

// stripText 依次执行「开头身份句」与「任意位置指纹」两类剥离。
func stripText(text string, cfg *StripConfig) (string, int, bool) {
	if cfg == nil || !cfg.Enabled {
		return text, 0, false
	}
	out, removed, hit := stripBlockedIdentity(text, cfg.Rules)
	if hit {
		text = out
	}
	rest, removed2, hit2 := stripBlockedContains(text, cfg.Contains)
	if hit2 {
		text = rest
	}
	return text, removed + removed2, hit || hit2
}

// cutBlockedPrefix 若 s 归一化后以某条规则开头，返回「切掉该句后」的余下内容。
//
// 切法：按 offsets 找到该句在原串中的结束 byte 偏移，然后跳过紧跟的
// 一个句末标点（. ! ?）与前后空白，余下部分原样返回。
func cutBlockedPrefix(s string, rules []string) (string, bool) {
	maxRuleLen := 0
	for _, r := range rules {
		if len(r) > maxRuleLen {
			maxRuleLen = len(r)
		}
	}
	if maxRuleLen == 0 {
		return s, false
	}
	norm := normalizeOnly(s, maxRuleLen)
	if norm == "" {
		return s, false
	}
	for _, rule := range rules {
		if rule == "" || !strings.HasPrefix(norm, rule) {
			continue
		}
		// 前缀剥离用不到「起始偏移」，只需要规则末字节在原串中的结束位置。
		start, prefixEnd, ok := mapNormIndex(s, maxRuleLen, 0, len(rule))
		_ = start
		if !ok {
			continue
		}
		end := prefixEnd
		// 跳过句末标点前允许的句内空白（不跨换行，避免误吞下一段首字符）
		for end < len(s) {
			r, sz := decodeRune(s[end:])
			if !normalizedSpace(r) {
				break
			}
			end += sz
		}
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

// ContainsForSite 返回该站点「任意位置」指纹的全量黑名单。
//
// 与 RulesForSite 同一口径：未知站点回落 global，且绝不 panic。
// global 目前没有 contains 规则，返回 nil 表示「该站点不做子串剥离」。
func ContainsForSite(site string) []string {
	return blockedContainsRules[config.NormalizeSite(site)]
}

// StripConfig 描述一次请求的剥离配置。
type StripConfig struct {
	Enabled bool
	// Rules 是「消息开头」命中的身份句（归一化形态）。
	Rules []string
	// Contains 是「任意位置」命中的指纹段落（归一化形态）。
	Contains []string
	// SkipAssistant 为 true 时不剥 assistant 正文（只剥 system）。
	//
	// 用「跳过」而不是「启用」做字段，是为了让零值保持既有行为：
	// 上游 11128 会扫 assistant，历史里出现指纹会让整段会话永久 502，
	// 而折叠 system 救不了它（只改 system）。所以默认必须剥。
	// 但它改的是「模型自己的历史输出」，语义风险高于改 system ——
	// 需要能单独关掉。
	SkipAssistant bool
}

// StripReport 回填一次请求实际发生的剥离情况，供网关记事件。
// 指针由调用方传入；为 nil 时不记录。
type StripReport struct {
	Stripped bool
	Bytes    int
	// SystemBytes / AssistantBytes 区分剥离发生在哪一侧。
	//
	// 为什么必须分开记：stripped=true 只说「删过东西」，但用户真正关心的是
	// 「我发给模型的 system 有没有被改」。只删 assistant 历史时 system 仍是
	// 100% 原样，这与「system 被削过」是完全不同的两件事，不能混成一个数字。
	SystemBytes    int
	AssistantBytes int
}

// add 累加一次剥离。isSystem 决定计入哪一侧。
func (r *StripReport) add(removed int, isSystem bool) {
	if r == nil || removed <= 0 {
		return
	}
	r.Stripped = true
	r.Bytes += removed
	if isSystem {
		r.SystemBytes += removed
	} else {
		r.AssistantBytes += removed
	}
}

// Scope 返回剥离发生的位置：system / assistant / both / 空串（没剥）。
func (r *StripReport) Scope() string {
	if r == nil || !r.Stripped {
		return ""
	}
	switch {
	case r.SystemBytes > 0 && r.AssistantBytes > 0:
		return "both"
	case r.SystemBytes > 0:
		return "system"
	case r.AssistantBytes > 0:
		return "assistant"
	default:
		return "both"
	}
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
func stripContent(content any, cfg *StripConfig, report *StripReport, isSystem bool) (any, bool) {
	if cfg == nil || !cfg.Enabled || (len(cfg.Rules) == 0 && len(cfg.Contains) == 0) {
		return content, false
	}
	switch v := content.(type) {
	case string:
		out, removed, hit := stripText(v, cfg)
		if !hit {
			return content, false
		}
		report.add(removed, isSystem)
		return out, true
	case []any:
		var outSlice []any
		for i, part := range v {
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
			out, removed, hit := stripText(text, cfg)
			if !hit {
				continue
			}
			if outSlice == nil {
				outSlice = append([]any(nil), v...)
			}
			cloned := maps.Clone(pm)
			cloned["text"] = out
			outSlice[i] = cloned
			report.add(removed, isSystem)
		}
		if outSlice == nil {
			return content, false
		}
		return outSlice, true
	default:
		return content, false
	}
}
