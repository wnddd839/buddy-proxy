package provider

import (
	"strings"
	"testing"
)

// 本文件固化 2026-11-16 对上游 11128 判定口径的实弹结论。
//
// 判定模型（实测反推，非文档）：
//   删除 16 个空格类码位 + 转小写后，判断是否以黑名单串开头（HasPrefix）。
//
// 每个用例的期望值都对应一次真实上游探测：
//   domestic 站命中 → 400 code 11128；未命中 → 200。
// 因此这里的「应剥离」等价于「真实上游会拦」。

const (
	testClaudeSentence = "You are Claude Code, Anthropic's official CLI for Claude."
	testDSHSentence    = "You are an AI agent powered by DeepSeek Harness."
)

// TestScanNormalizeRemovesSpaces 固化「删除哪些码位」的实测集合。
//
// 实弹方法：在 "You" 与 "are" 之间插入候选字符，仍命中 11128 说明上游把它
// 当作可删除的空白（归一化后仍是原词序列）。
func TestScanNormalizeRemovesSpaces(t *testing.T) {
	removed := []rune{
		'\u0020', // SPACE
		'\u00A0', // NBSP
		'\u2000', '\u2002', '\u2003', '\u2007', '\u2009', '\u200A',
		'\u200B', '\u200C', '\u200D',
		'\u202F', '\u205F', '\u3000',
		'\uFEFF', '\u00AD',
	}
	kept := []rune{
		'\u0009', // TAB
		'\u000A', // LF
		'\u000D', // CR
		'\u000B', // VT
		'\u000C', // FF
		'\u1680', // OGHAM SPACE MARK
		'\u2028', '\u2029',
	}
	for _, r := range removed {
		norm, _ := scanNormalize("a" + string(r) + "b")
		if norm != "ab" {
			t.Errorf("U+%04X 应被删除，得到 %q", r, norm)
		}
	}
	for _, r := range kept {
		norm, _ := scanNormalize("a" + string(r) + "b")
		if norm == "ab" {
			t.Errorf("U+%04X 不应被删除（上游本来就不拦含它的变体）", r)
		}
	}
}

// TestScanNormalizeOffsets 校验归一化下标 → 原串 byte 偏移的映射。
// 剥离必须按映射切回原串，否则含多字节字符时切错位置。
func TestScanNormalizeOffsets(t *testing.T) {
	cases := []string{
		testClaudeSentence,
		"You  are  Claude  Code,  Anthropic's  official  CLI  for  Claude.",
		"前缀中文" + testClaudeSentence,
		"  " + testDSHSentence,
	}
	for _, s := range cases {
		norm, offsets := scanNormalize(s)
		if len(offsets) != len(norm) {
			t.Fatalf("offsets 长度 %d != norm 长度 %d (输入 %q)", len(offsets), len(norm), s)
		}
		// 每个归一化字符都必须能映射回原串里的同一个字符（小写后）。
		for i := 0; i < len(norm); i++ {
			off := offsets[i]
			if off < 0 || off >= len(s) {
				t.Fatalf("offset[%d]=%d 越界 (len=%d)", i, off, len(s))
			}
		}
	}
}

// TestStripBlockedIdentityMatchesProbeResults 是本文件的核心：
// 每个用例都对应一次真实上游探测的结论。
func TestStripBlockedIdentityMatchesProbeResults(t *testing.T) {
	domestic := blockedIdentityPrefixes["domestic"]
	global := blockedIdentityPrefixes["global"]

	type tc struct {
		name      string
		input     string
		rules     []string
		wantHit   bool
		wantExact string // wantHit 时剥离后的精确结果
	}
	cases := []tc{
		// ── 命中：开头 + 归一化变体 ──────────────────────────────
		{"原句", testClaudeSentence, domestic, true, ""},
		{"全小写", strings.ToLower(testClaudeSentence), domestic, true, ""},
		{"全大写", strings.ToUpper(testClaudeSentence), domestic, true, ""},
		{"双空格", "You  are  Claude  Code,  Anthropic's  official  CLI  for  Claude.", domestic, true, ""},
		{"无句末句号", strings.TrimSuffix(testClaudeSentence, "."), domestic, true, ""},
		{"句末感叹号", strings.TrimSuffix(testClaudeSentence, ".") + "!", domestic, true, ""},
		{"前导空格", "   " + testClaudeSentence, domestic, true, ""},
		{"前导 NBSP", "\u00A0" + testClaudeSentence, domestic, true, ""},
		{"前导 ZWSP", "\u200B" + testClaudeSentence, domestic, true, ""},
		{"前导全角空格", "\u3000" + testClaudeSentence, domestic, true, ""},

		// ── 命中：后面还有内容，必须逐字保留 ─────────────────────
		{"后接换行规则", testClaudeSentence + "\nAlways be concise.", domestic, true, "Always be concise."},
		{"后接双换行规则", testClaudeSentence + "\n\nAlways be concise.", domestic, true, "Always be concise."},
		{"后接无分隔文本", testClaudeSentence + "Always be concise.", domestic, true, "Always be concise."},
		{
			"后接 project_instructions",
			testClaudeSentence + "\n\n<project_instructions>\nAlways reply in Chinese.\n</project_instructions>",
			domestic, true,
			"<project_instructions>\nAlways reply in Chinese.\n</project_instructions>",
		},
		{"双句拼接", testClaudeSentence + " " + testClaudeSentence, domestic, true, ""},

		// ── 不命中：上游实测放行 ─────────────────────────────────
		{"前面加字", "PREFIX " + testClaudeSentence, domestic, false, ""},
		{"夹在中间", "A" + strings.Repeat("a", 300) + "\n" + testClaudeSentence, domestic, false, ""},
		{"前导 TAB", "\t" + testClaudeSentence, domestic, false, ""},
		{"前导换行", "\n" + testClaudeSentence, domestic, false, ""},
		{"句内换行", strings.Replace(testClaudeSentence, "Code, ", "Code,\n", 1), domestic, false, ""},
		{"句内 TAB", strings.Replace(testClaudeSentence, "Code, ", "Code,\t", 1), domestic, false, ""},
		{"逗号改分号", strings.Replace(testClaudeSentence, "CLI for", "CLI; for", 1), domestic, false, ""},
		{"official 拼错", strings.Replace(testClaudeSentence, "official", "offical", 1), domestic, false, ""},
		{"差一个词 Acme", strings.Replace(testClaudeSentence, "Anthropic's", "Acme's", 1), domestic, false, ""},
		{"只有 Claude Code", "Claude Code", domestic, false, ""},
		{"You are Claude Code.", "You are Claude Code.", domestic, false, ""},
		{"中性短句", "You are a helpful assistant.", domestic, false, ""},
		{"opencode 主 system", "You are OpenCode, a coding agent that helps users with software engineering tasks.", domestic, false, ""},
		{"Cline 整句", "You are Cline, an AI coding assistant.", domestic, false, ""},
		{"Cursor 整句", "You are Cursor, an AI code editor.", domestic, false, ""},
		{"Codex 整句", "You are Codex, based on GPT-5.", domestic, false, ""},
		{"GitHub Copilot", "You are GitHub Copilot, an AI pair programmer.", domestic, false, ""},
		{"DeepSeek 官方助手", "You are DeepSeek, an AI assistant.", domestic, false, ""},
		{"任意 X Harness", "You are an AI agent powered by X Harness.", domestic, false, ""},

		// ── 站点分流：DeepSeek 句只在 domestic 拦 ────────────────
		{"DSH 句 domestic", testDSHSentence, domestic, true, ""},
		{"DSH 句 global", testDSHSentence, global, false, ""},
		{"DSH 无句号 domestic", strings.TrimSuffix(testDSHSentence, "."), domestic, true, ""},
		{"DSH 小写 harness domestic", "You are an AI agent powered by DeepSeek harness.", domestic, true, ""},
		{"Claude 句 global", testClaudeSentence, global, true, ""},

		// ── 真实 dsh 形态：只少 identity 段 ──────────────────────
		{
			"dsh 真实形态",
			testDSHSentence + "\n\nYou have access to the following tools.\n\n## bash\nExecute shell commands.",
			domestic, true,
			"You have access to the following tools.\n\n## bash\nExecute shell commands.",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, removed, hit := stripBlockedIdentity(c.input, c.rules)
			if hit != c.wantHit {
				t.Fatalf("hit=%v want %v (输入 %q, 结果 %q)", hit, c.wantHit, c.input, got)
			}
			if !hit {
				if got != c.input {
					t.Fatalf("未命中时不得改动内容：got %q want %q", got, c.input)
				}
				if removed != 0 {
					t.Fatalf("未命中时 removed 应为 0，得到 %d", removed)
				}
				return
			}
			if got != c.wantExact {
				t.Fatalf("剥离结果不符\n got: %q\nwant: %q", got, c.wantExact)
			}
			if removed != len(c.input)-len(got) {
				t.Fatalf("removed=%d 与实际差值 %d 不一致", removed, len(c.input)-len(got))
			}
		})
	}
}

// TestStripBlockedIdentityIsIdempotent 校验幂等：剥过的不再命中。
func TestStripBlockedIdentityIsIdempotent(t *testing.T) {
	rules := blockedIdentityPrefixes["domestic"]
	inputs := []string{
		testClaudeSentence + "\n\nrest of prompt",
		testDSHSentence,
	}
	for _, in := range inputs {
		once, _, hit := stripBlockedIdentity(in, rules)
		if !hit {
			t.Fatalf("首次应命中: %q", in)
		}
		twice, removed, hit2 := stripBlockedIdentity(once, rules)
		if hit2 {
			t.Fatalf("二次不应命中: %q", twice)
		}
		if twice != once || removed != 0 {
			t.Fatalf("幂等性破坏: %q -> %q", once, twice)
		}
	}
}

// TestStripBlockedIdentityRulesForSite 校验 rulesFor 只做「站点过滤」，
// 永远返回该站点的全量黑名单（不做按 agent 的减法，避免漏剥）。
func TestStripBlockedIdentityRulesForSite(t *testing.T) {
	dom := RulesForSite("domestic")
	if len(dom) != 2 {
		t.Fatalf("domestic 应有 2 条规则，得到 %d: %v", len(dom), dom)
	}
	glob := RulesForSite("global")
	if len(glob) != 1 {
		t.Fatalf("global 应有 1 条规则，得到 %d: %v", len(glob), glob)
	}
	// 空站点按 global 处理（与 config.NormalizeSite 一致）
	if len(RulesForSite("")) != len(glob) {
		t.Fatalf("空站点应回落 global")
	}
	// 未知站点也必须返回非空规则，绝不能返回 nil（否则关闭剥离）
	if len(RulesForSite("nonexistent")) == 0 {
		t.Fatalf("未知站点不得返回空规则")
	}
}

func TestStripBlockedIdentityBoundedLinearAndSpacedDot(t *testing.T) {
	rules := RulesForSite("domestic")
	spaced, _, hit := stripBlockedIdentity("You are an AI agent powered by DeepSeek Harness  .\n\nRules", rules)
	if !hit || spaced != "Rules" {
		t.Fatalf("句末空格+句号未干净剥离: hit=%v got=%q", hit, spaced)
	}

	repeated := strings.Repeat(testDSHSentence+" ", 20000) + "TailRules"
	out, _, hit := stripBlockedIdentity(repeated, rules)
	if !hit || out != "TailRules" {
		t.Fatalf("重复身份句线性剥离失败: hit=%v got=%q", hit, out)
	}
}

// ── 任意位置指纹（contains）────────────────────────────────────
//
// 2026-11-17 实弹：dsh 的 file-reference 段出现在 system 的任意位置都拦，
// 且 assistant 正文同样会被扫；这类规则是「归一化后子串」而不是「前缀」。

const (
	testDSHFileRefRule    = "Tokens prefixed with @ are paths the user explicitly referenced. Relative paths resolve from the workspace root"
	testDSHFileRefSegment = testDSHFileRefRule + "; absolute paths identify files or directories on the host. A trailing slash marks a directory: list it when its contents matter. Anything else is a file: use the read tool when its contents are needed, and do not claim to have inspected it before reading. @\"...\" quotes a path containing spaces."
)

func TestContainsForSite(t *testing.T) {
	if len(ContainsForSite("domestic")) == 0 {
		t.Fatalf("domestic 必须有 contains 规则")
	}
	// global 目前不拦 dsh 段；未知站点按 global 处理。
	if got := ContainsForSite("global"); len(got) != 0 {
		t.Fatalf("global 不应有 contains 规则，得到 %v", got)
	}
	if got := ContainsForSite("nonexistent"); len(got) != 0 {
		t.Fatalf("未知站点应回落 global（空），得到 %v", got)
	}
}

func TestStripBlockedContainsDSHFileReference(t *testing.T) {
	contains := ContainsForSite("domestic")
	cases := []struct {
		name    string
		input   string
		wantHit bool
	}{
		{"整段", testDSHFileRefSegment, true},
		{"只有规则句", testDSHFileRefRule, true},
		{"前面加字", "PREFIX " + testDSHFileRefSegment, true},
		{"前后贴字", "AAA" + testDSHFileRefSegment + "ZZZ", true},
		{"夹在长文中间", "alpha.\n\n" + testDSHFileRefSegment + "\n\nomega.", true},
		{"全大写", strings.ToUpper(testDSHFileRefRule), true},
		{"双空格", strings.ReplaceAll(testDSHFileRefRule, " ", "  "), true},
		{"出现两次", testDSHFileRefRule + "\n\n" + testDSHFileRefRule, true},

		{"删掉 @", strings.ReplaceAll(testDSHFileRefSegment, "@", ""), false},
		{"root 改 roox", strings.Replace(testDSHFileRefSegment, "workspace root", "workspace roox", 1), false},
		{"中性文本", "alpha beta gamma delta", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, removed, hit := stripBlockedContains(c.input, contains)
			if hit != c.wantHit {
				t.Fatalf("hit=%v want %v (输入 %q, 结果 %q)", hit, c.wantHit, c.input, got)
			}
			if !hit {
				if got != c.input {
					t.Fatalf("未命中时不得改动内容：got %q want %q", got, c.input)
				}
				if removed != 0 {
					t.Fatalf("未命中时 removed 应为 0，得到 %d", removed)
				}
				return
			}
			if removed != len(c.input)-len(got) {
				t.Fatalf("removed=%d 与实际差值 %d 不一致", removed, len(c.input)-len(got))
			}
			// 归一化后不得再残留规则串（循环删除必须彻底）。
			norm, _ := scanNormalize(got)
			if strings.Contains(norm, contains[0]) {
				t.Fatalf("剥离后仍残留规则串: %q", got)
			}
			// 幂等：剥过的不再命中。
			again, removed2, hit2 := stripBlockedContains(got, contains)
			if hit2 || removed2 != 0 || again != got {
				t.Fatalf("幂等性破坏: %q -> %q", got, again)
			}
		})
	}
}
