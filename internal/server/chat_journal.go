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
	// reasoningRequested 是「客户端到底有没有要求思考」的唯一权威判定：
	// provider.ReasoningRequested 会看 thinking:{type:disabled}，
	// 而 effortTier 只看档位字符串。
	//
	// 为什么必须先用它过滤 tier：客户端同时发 reasoning_effort:"high" 与
	// thinking:{"type":"disabled"} 时，只看档位会记成 tier=high 并纳入 I12 ——
	// 但模型正确地不思考（客户端自己关的），会被误判成「退化」。
	// （评审 Important 5）
	reasoningRequested := provider.ReasoningRequested(provider.ChatOptions{
		ReasoningEffort: opts.ReasoningEffort, Reasoning: opts.Reasoning, Thinking: opts.Thinking,
	})
	reasoningTier := ""
	if reasoningRequested {
		reasoningTier = effortTier(effectiveEffort(opts.ReasoningEffort, opts.Reasoning))
	}

	input := gateway.ChatRecordInput{
		Started:        started,
		ProxyRequestID: proxyID,
		SessionKey:     opts.SessionKey,
		SessionLabel:   opts.SessionLabel,
		Model:          strutil.First(publicModel, opts.Model, "auto"),
		// 实际发给上游的模型：与客户端请求的不同时才有意义（标题改写 / 模型路由）。
		RoutedModel: func() string {
			if opts.RoutedFrom != "" && opts.Model != "" {
				return opts.Model
			}
			return ""
		}(),
		Stream: stream,
		Ok:     ok,
		// 成功但没有结果的记录来自客户端取消，仍记账，但不作为完成样本监测。
		Canceled:           ok && result == nil,
		ErrMsg:             strings.TrimSpace(errMsg),
		Usage:              usage,
		ReasoningRequested: reasoningRequested,
		// 原值 + 实际档位并排落盘：前者回答「客户端要了什么」，
		// 后者回答「上游真按哪档跑」——拼错的档位只有并排才看得出来。
		//
		// tier 只在**真的要求了思考**时才有值（见上方 reasoningRequested 的注释）。
		ReasoningEffort: effectiveEffort(opts.ReasoningEffort, opts.Reasoning),
		ReasoningTier:   reasoningTier,
	}
	if result != nil {
		input.Trace = result.Trace
		input.Account = result.Account
		input.AccountID = result.AccountID
		input.RoutedModel = result.RoutedModel
	}
	// system 形态由 CompleteFromPool 回填到共享指针里；同一行用量明细能显示
	// keep / fold / fold(降级)，与 06 面板按 ProxyRequestID 互查。
	if outcome := opts.SystemOutcome; outcome != nil {
		input.SystemForm = outcome.Form
		input.SystemFp = outcome.Fingerprint
		input.SystemDegraded = outcome.Degraded
		input.SystemStripped = outcome.Stripped
		input.StripScope = outcome.StripScope
		input.IO = outcome.IO
		// OutObserved 是「出站确实发生过」的显式证据（provider 在构造出站
		// payload 之后置位）。没有它，I3/I4 分不开「没发」与「发出去但丢光了」。
		if outcome.IO != nil {
			input.OutObserved = outcome.IO.OutObserved
		}
	}
	if result != nil {
		input.UpstreamModel = result.UpstreamModel
	}
	// 分词器指纹挂在 SystemOutcome.IO 上（CompleteFromPool 每轮共享同一指针）。
	if outcome := opts.SystemOutcome; outcome != nil {
		input.Tok = outcome.IO
	}
	// 带 tools 的请求：prompt_tokens 含工具定义，入站字符数不含 ⇒
	// I10/I11 的比例判据会被虚高污染，必须标记。
	input.HasTools = opts.Tools != nil
	// system 被复制进 user 时同理（分子多一份 system 长度）。
	input.SystemEchoed = s.Svc.TokFingerprintEchoed()
	input.TitleRewrite = opts.RoutedFrom != "" && opts.TitleRewrite
	input.UsageSource = usage.Source
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
