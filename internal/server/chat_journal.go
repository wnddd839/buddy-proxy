package server

import (
	"strings"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/gateway"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/strutil"
)

func (s *Server) newProxyRequestID() string {
	return strutil.RandomHex(16)
}

func (s *Server) recordChatJournal(
	started time.Time,
	proxyID string,
	opts gateway.CompleteOptions,
	publicModel string,
	stream bool,
	ok bool,
	errMsg string,
	usage provider.Usage,
	result *gateway.CompleteResult,
) {
	if s == nil || s.Svc == nil {
		return
	}
	input := gateway.ChatRecordInput{
		Started:        started,
		ProxyRequestID: proxyID,
		SessionKey:     opts.SessionKey,
		SessionLabel:   opts.SessionLabel,
		Model:          strutil.First(publicModel, opts.Model, "auto"),
		Stream:         stream,
		Ok:             ok,
		// 成功但没有结果的记录来自客户端取消，仍记账，但不作为完成样本监测。
		Canceled: ok && result == nil,
		ErrMsg:   strings.TrimSpace(errMsg),
		Usage:    usage,
		ReasoningRequested: provider.ReasoningRequested(provider.ChatOptions{
			ReasoningEffort: opts.ReasoningEffort, Reasoning: opts.Reasoning, Thinking: opts.Thinking,
		}),
		// 原值 + 实际档位并排落盘：前者回答「客户端要了什么」，
		// 后者回答「上游真按哪档跑」——拼错的档位只有并排才看得出来。
		ReasoningEffort: effectiveEffort(opts.ReasoningEffort, opts.Reasoning),
		ReasoningTier:   effortTier(effectiveEffort(opts.ReasoningEffort, opts.Reasoning)),
	}
	if result != nil {
		input.Trace = result.Trace
		input.Account = result.Account
		input.AccountID = result.AccountID
	}
	// system 形态由 CompleteFromPool 回填到共享指针里；同一行用量明细能显示
	// keep / fold / fold(降级)，与 06 面板按 ProxyRequestID 互查。
	if outcome := opts.SystemOutcome; outcome != nil {
		input.SystemForm = outcome.Form
		input.SystemFp = outcome.Fingerprint
		input.SystemDegraded = outcome.Degraded
	}
	s.Svc.RecordChat(input)
}

// effectiveEffort 取客户端实际生效的档位：扁平字段优先，其次对象里的 effort。
// 与 provider.ApplyReasoningFields 的取值顺序保持一致。
func effectiveEffort(flat string, obj map[string]any) string {
	if v := strings.TrimSpace(flat); v != "" {
		return v
	}
	if obj != nil {
		if raw, ok := obj["effort"]; ok {
			if s, ok := raw.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

// effortTier 把客户端原值换算成上游实际档位（low/high/max）。
// 与 provider.ApplyReasoningFields 写进 body 的值保持一致；
// 客户端没传或明确关掉思考时返回空，避免把「不思考」记成某一档。
func effortTier(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	wire, _ := provider.CanonicalReasoningEffort(raw)
	switch wire {
	case provider.EffortLow, provider.EffortHigh, provider.EffortMax:
		return wire
	}
	// none/off/disabled 之类：上游确实不思考，不归入任何档位。
	return ""
}
