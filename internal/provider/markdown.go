package provider

import "strings"

// atxFixer 在行首把 `##标题` 修成 `## 标题`（CommonMark ATX 必须有空格）。
// GLM 等模型常省略该空格，客户端就把 ## 当纯文本；CLIProxyAPI 一类代理会补空格。
type atxFixer struct {
	lineStart bool
	hashes    int
}

func newATXFixer() *atxFixer {
	return &atxFixer{lineStart: true}
}

func (f *atxFixer) Feed(s string) string {
	if f == nil || s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 4)
	for _, r := range s {
		if f.hashes > 0 {
			switch {
			case r == '#' && f.hashes < 6:
				f.hashes++
			case r != ' ' && r != '\t' && r != '\n' && r != '\r' && r != '#':
				// 只修 ##–######；单个 # 留给 #include / shebang / #define。
				if f.hashes >= 2 {
					b.WriteByte(' ')
				}
				f.hashes = 0
				f.lineStart = false
			default:
				f.hashes = 0
				if r != '\n' && r != '\r' {
					f.lineStart = false
				}
			}
		} else if f.lineStart && r == '#' {
			f.hashes = 1
		} else if r != '\n' && r != '\r' {
			f.lineStart = false
		}
		b.WriteRune(r)
		if r == '\n' {
			f.lineStart = true
			f.hashes = 0
		}
	}
	return b.String()
}
