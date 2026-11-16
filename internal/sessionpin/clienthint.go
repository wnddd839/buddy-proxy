// Package sessionpin 的客户端识别。
//
// 设计意图（重要）
// --------------
// 本文件只负责「从请求头推断是哪个客户端」，**不参与任何功能决策**。
// 具体来说：
//
//   - 上游 11128 身份句剥离：只看 system 内容（见 provider.identity_strip.go），
//     与这里的识别结果无关。原因：User-Agent 可伪造/可缺失/会被 SDK 覆盖，
//     一旦按它决定剥不剥，识别失败就等于漏剥，请求仍会被上游拒收。
//   - 会话亲和：只看显式会话头（pin.go 的候选列表），不看 UA。
//
// 识别结果只用于：
//  1. 事件与面板展示「这条请求来自哪类客户端」，便于排查
//  2. 未来需要按客户端做差异化观测时作为输入
//
// 因此这里的误判不会影响请求正确性，最坏只是观测标签不准。
package sessionpin

import (
	"net/http"
	"strings"
)

// 客户端类型常量。空串表示「识别不出」，用于避免污染事件。
const (
	ClientClaudeCode      = "claude-code"
	ClientOpencode        = "opencode"
	ClientDeepSeekHarness = "deepseek-harness"
	ClientCline           = "cline"
	ClientCursor          = "cursor"
	ClientCodex           = "codex"
	ClientOpenAISDK       = "openai-sdk"
	ClientUnknown         = "unknown"
)

// ClientHint 从请求头推断客户端类型。
//
// 判定顺序（顺序本身是正确性的一部分）：
//
//	① 专属请求头 —— 最可靠，格式固定、不可伪造
//	② 专属 UA 前缀 —— 各 agent 的官方 UA 格式
//	③ 弱信号（anthropic-version + x-app）—— 必须排在 ① 之后
//
// 为什么 ③ 必须靠后：DeepSeek Harness 也会发
// anthropic-version: 2023-06-01 与 anthropic-beta（源码实证），
// 若先判 anthropic-version，dsh 会被误识别成 claude-code。
//
// 返回：
//   - 具体客户端常量：识别成功
//   - ClientUnknown：完全没有 UA（明确的「未知」）
//   - "": 有 UA 但不认识（不记录，避免事件被随机客户端淹没）
func ClientHint(h http.Header) string {
	if h == nil {
		return ClientUnknown
	}

	// ① 专属头优先。注意 dsh 的判定必须早于任何 anthropic-* 判定。
	if h.Get("X-Deepseek-Harness-Session-Id") != "" ||
		h.Get("X-Dsh-Auth-Token") != "" ||
		h.Get("X-Deepseek-Harness-User-Id") != "" {
		return ClientDeepSeekHarness
	}
	if h.Get("X-Opencode-Client") != "" || h.Get("X-Opencode-Session") != "" {
		return ClientOpencode
	}

	// ② 专属 UA 前缀。
	ua := strings.ToLower(strings.TrimSpace(h.Get("User-Agent")))
	switch {
	case strings.HasPrefix(ua, "deepseek-harness/"):
		return ClientDeepSeekHarness
	case strings.HasPrefix(ua, "opencode/"):
		return ClientOpencode
	case strings.HasPrefix(ua, "claude-cli/"):
		return ClientClaudeCode
	case strings.HasPrefix(ua, "cline"):
		return ClientCline
	case strings.HasPrefix(ua, "cursor"):
		return ClientCursor
	case strings.HasPrefix(ua, "codex"):
		return ClientCodex
	case strings.HasPrefix(ua, "openai/"):
		return ClientOpenAISDK
	}

	// ③ 弱信号：Claude Code 的 x-app: cli 是它独有的（各 SDK 不设这个头），
	// 配合 anthropic-version 才认定。单独 anthropic-version 不成立 ——
	// 很多 Anthropic 兼容网关都会发。
	if strings.EqualFold(h.Get("X-App"), "cli") && h.Get("Anthropic-Version") != "" {
		return ClientClaudeCode
	}

	if ua == "" {
		return ClientUnknown
	}
	return ""
}
