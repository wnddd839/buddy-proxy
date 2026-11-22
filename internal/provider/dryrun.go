package provider

import "strings"

// DryRunOptions 是「预览网关会怎么改这条请求」的输入。
type DryRunOptions struct {
	Messages []map[string]any
	// Site 决定是否补内置 system（global 要求首条是 system）。
	Site string
	// KeepSystem 对应生产里的 keepSystemFor(opts)：
	// auto / keep / strip 为 true（直传 system），fold 为 false（折进 user）。
	KeepSystem bool
	// Strip 非 nil 且 Enabled 时按已知指纹剥离。
	Strip *StripConfig
	// EchoSystemToUser 开启时把 system 复制进首条 user。
	EchoSystemToUser bool
	// ReasoningReplay 控制历史 assistant 的 reasoning_content 是否回传。
	ReasoningReplay bool
}

// DryRunResult 是预览结果。
type DryRunResult struct {
	Outbound     []map[string]any `json:"outbound"`
	Report       StripReport      `json:"stripReport"`
	InMessages   int              `json:"inMessages"`
	OutMessages  int              `json:"outMessages"`
	InSystemLen  int              `json:"inSystemLen"`
	OutSystemLen int              `json:"outSystemLen"`
	Notes        []string         `json:"notes"`
	Warnings     []string         `json:"warnings"`
}

// DryRun 在不发请求的前提下，预览「网关最终会发给上游什么」。
//
// ## 为什么需要它
//
// 现有验证都是**事后**的：06 事件流、决策日志。排查「我这个 system
// 会不会被剥 / 被折 / 被 echo 复制」时需要**事前**答案，而且不能花钱。
//
// ## 一致性保证
//
// 复用**同一条生产代码路径**（EnsureUpstreamMessagesWithReplay +
// echoSystemIntoFirstUser），不是另写一套近似逻辑 —— 预览失真比没有更糟。
// 唯一差别是不发 HTTP、不回填 usage。
//
// fold 不需要单独实现：keepSystem=false 时生产函数自己会做折叠
// （system 换成 canonicalUpstreamSystem + 原 system 包进 <client_instructions>
// 挪到首条 user）。所以这里只传 KeepSystem，不另设 FoldSystem。
func DryRun(opts DryRunOptions) DryRunResult {
	report := &StripReport{}
	inIO := MessagesIO{}
	FillMessagesIO(&inIO, opts.Messages, nil)

	messages := opts.Messages
	var notes []string

	messages = EnsureUpstreamMessagesWithReplay(
		messages, opts.KeepSystem, opts.Strip, report, opts.ReasoningReplay,
	)

	switch {
	case opts.KeepSystem && opts.Strip != nil && opts.Strip.Enabled:
		notes = append(notes, "keep + strip：system 直传，已知指纹被剥离")
	case !opts.KeepSystem:
		notes = append(notes, "fold：原 system 包进 <client_instructions> 挪到首条 user，system 位换成内置人设")
	case opts.KeepSystem:
		notes = append(notes, "keep：system 直传，不剥离")
	}

	if opts.EchoSystemToUser && opts.KeepSystem {
		messages = echoSystemIntoFirstUser(messages)
		notes = append(notes, "echo：system 复制一份进首条 user（多消耗约 system 长度的 prompt token）")
	}

	// global 站要求首条是 system；缺失时补内置。
	if normalizeSiteLower(opts.Site) == "global" && len(messages) > 0 &&
		NormalizeMessageRole(fmtStr(messages[0]["role"])) != "system" {
		out := make([]map[string]any, 0, len(messages)+1)
		out = append(out, map[string]any{"role": "system", "content": canonicalUpstreamSystem})
		messages = append(out, messages...)
		notes = append(notes, "global 注入：首条不是 system，补入内置 system")
	}

	io := MessagesIO{}
	FillMessagesIO(&io, opts.Messages, messages)

	res := DryRunResult{
		Outbound:     messages,
		Report:       *report,
		InMessages:   inIO.InMessages,
		OutMessages:  io.OutMessages,
		InSystemLen:  inIO.InSystemLen,
		OutSystemLen: io.OutSystemLen,
		Notes:        notes,
	}

	if res.InSystemLen > 0 && res.OutSystemLen == 0 {
		res.Warnings = append(res.Warnings,
			"出站 system 为空 —— 客户端的 system 不会到达模型（检查 fold / strip 档位）")
	}
	if report.SystemBytes > 0 {
		res.Warnings = append(res.Warnings,
			"system 内容被剥离 —— 模型看到的与客户端发的不一致")
	}
	if !opts.KeepSystem && res.InMessages > 0 && res.OutMessages == res.InMessages {
		res.Warnings = append(res.Warnings,
			"fold 档但条数没变 —— 客户端可能本来就没有 system，折叠未生效")
	}

	return res
}

// normalizeSiteLower 小写化（避免 import config 造成包循环）。
func normalizeSiteLower(site string) string {
	s := strings.ToLower(strings.TrimSpace(site))
	if s == "" {
		return "global"
	}
	return s
}

func fmtStr(v any) string {
	s, _ := v.(string)
	return s
}
