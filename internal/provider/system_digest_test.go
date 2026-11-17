package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// 与 internal/gateway/systemprompt.go 的 systemPromptText / systemPromptDigest
// 以及 internal/systemprompt.Fingerprint 完全同语义的复刻（provider 包不能
// import gateway，否则循环）。用于锁住 keep / fold 两条路径的指纹口径一致。
func testSystemText(content any) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(raw)
	}
}

func testSystemDigest(messages []map[string]any) string {
	var parts []string
	for _, m := range messages {
		if NormalizeMessageRole(strings.TrimSpace(asTestString(m["role"]))) != "system" {
			continue
		}
		text := strings.TrimSpace(testSystemText(m["content"]))
		if text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	joined := strings.Join(parts, "\n\n")
	sum := sha256.Sum256([]byte(joined))
	return hex.EncodeToString(sum[:4])
}

func asTestString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// extractClientInstructions 取出 fold 后 user 消息里 <client_instructions> 包裹的纯文本。
func extractClientInstructions(t *testing.T, out []map[string]any) string {
	t.Helper()
	const open = "<client_instructions>\n"
	const close = "\n</client_instructions>"
	for _, m := range out {
		if asTestString(m["role"]) != "user" {
			continue
		}
		s, ok := m["content"].(string)
		if !ok {
			continue
		}
		start := strings.Index(s, open)
		end := strings.Index(s, close)
		if start < 0 || end < start {
			continue
		}
		return s[start+len(open) : end]
	}
	return ""
}

// TestStructuredSystemFoldMatchesGatewayDigest 锁住问题 #4：
// 结构化（多模态数组）system 在 keep 与 fold 两条事件的文本表示 /
// 指纹必须一致。旧实现 fold 用「normalizeContent 后的 item content」
// 经 fmt.Sprint 得到 "[map[type:text ...]]"，而 gateway 用原始 content
// 经 json.Marshal 得到 `[{"type":"text","text":"..."}]`，两者分叉。
func TestStructuredSystemFoldMatchesGatewayDigest(t *testing.T) {
	structured := []any{
		map[string]any{"type": "text", "text": "You are a helpful coding assistant."},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.test/a.png"}},
	}
	msgs := []map[string]any{
		{"role": "system", "content": structured},
		{"role": "user", "content": "hello"},
	}

	// gateway 侧：对原始 opts.Messages 计算的指纹与文本表示
	wantDigest := testSystemDigest(msgs)
	wantText := testSystemText(structured)
	if wantDigest == "" || wantText == "" {
		t.Fatal("测试基线：结构化 system 指纹 / 文本不应为空")
	}
	if !strings.HasPrefix(wantText, `[{`) {
		t.Fatalf("测试基线：gateway 口径应是 JSON，实际 %q", wantText)
	}

	// keep 路径：system 内容保留在 out[0]，文本表示不应被改动
	keep := EnsureUpstreamMessagesWith(msgs, true)
	if len(keep) == 0 || asTestString(keep[0]["role"]) != "system" {
		t.Fatalf("keep 首条应为 system，实际 %#v", keep)
	}
	keepText := testSystemText(keep[0]["content"])
	if keepText != wantText {
		t.Fatalf("keep 改变了 system 文本表示\n got=%q\nwant=%q", keepText, wantText)
	}
	if got := testSystemDigest(keep); got != wantDigest {
		t.Fatalf("keep 指纹分叉: got=%s want=%s", got, wantDigest)
	}

	// fold 路径：system 折进 user 的 <client_instructions>
	fold := EnsureUpstreamMessagesWith(msgs, false)
	if len(fold) == 0 || asTestString(fold[0]["content"]) != canonicalUpstreamSystem {
		t.Fatalf("fold 首条应为 canonical system，实际 %#v", fold)
	}
	inner := extractClientInstructions(t, fold)
	if inner == "" {
		t.Fatalf("fold 未生成 <client_instructions>: %#v", fold)
	}
	// 折叠内容必须与 gateway 侧 system 文本表示逐字节一致
	if inner != wantText {
		t.Fatalf("fold 的 instructions 文本表示与 gateway 分叉\n inner=%q\n want=%q\n"+
			"（旧实现会是 [map[type:text text:...]] 而非 JSON）", inner, wantText)
	}
	// 旧口径特征字符串不能出现
	if strings.Contains(inner, "map[") {
		t.Fatalf("fold 仍在用 fmt.Sprint 的 map[] 表示: %q", inner)
	}
	// 用 gateway 算法对折叠出的 system 原文复算指纹，必须等于原始 / keep 的指纹
	foldDigest := testSystemDigest([]map[string]any{{"role": "system", "content": inner}})
	if foldDigest != wantDigest {
		t.Fatalf("fold 指纹分叉: got=%s want=%s", foldDigest, wantDigest)
	}
}

// TestSystemDigestTextMatchesGateway 直接锁住导出函数与 gateway 语义一致：
// string 原样、nil 空、其它 json.Marshal（含键序稳定）。
func TestSystemDigestTextMatchesGateway(t *testing.T) {
	cases := []struct {
		name    string
		content any
		want    string
	}{
		{"string", "  raw system  ", "  raw system  "},
		{"nil", nil, ""},
		// json.Marshal 对 map 键按字母序输出，这里用同算法生成期望值，
		// 重点锁住「不是 fmt.Sprint 的 map[] 表示」。
		{"array", []any{map[string]any{"type": "text", "text": "hi"}}, ""},
		{"map", map[string]any{"a": 1}, `{"a":1}`},
	}
	marshaled, _ := json.Marshal(cases[2].content)
	cases[2].want = string(marshaled)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SystemDigestText(tc.content)
			if got != tc.want {
				t.Fatalf("got=%q want=%q", got, tc.want)
			}
			if got != testSystemText(tc.content) {
				t.Fatalf("与 gateway.systemPromptText 语义不一致: got=%q", got)
			}
		})
	}
}

// TestFoldStringSystemUnchanged 保证纯字符串 system 行为不回归：
// 旧/新口径对 string 都返回原样，折叠文本不变。
func TestFoldStringSystemUnchanged(t *testing.T) {
	msgs := []map[string]any{
		{"role": "system", "content": "You are opencode."},
		{"role": "user", "content": "hi"},
	}
	out := EnsureUpstreamMessagesWith(msgs, false)
	inner := extractClientInstructions(t, out)
	if inner != "You are opencode." {
		t.Fatalf("string system 折叠回归: inner=%q out=%#v", inner, out)
	}
}
