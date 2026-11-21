package gateway

import (
	"strings"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/decision"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/usagejournal"
	"github.com/wnddd839/codebuddy-proxy/internal/watch"
)

// ChatRecordInput is one gateway-visible chat completion for the usage journal.
type ChatRecordInput struct {
	Started        time.Time
	ProxyRequestID string
	SessionKey     string
	SessionLabel   string
	Model          string
	// RoutedModel 是实际发给上游的模型名（被模型路由或标题改写改过时才有值）。
	// 与 Model 并列而不是覆盖它：前者回答「客户端要了什么」，后者回答
	// 「真正跑的是哪个」—— 改写是否生效只有并排才看得出来。
	RoutedModel string
	Stream      bool
	Ok          bool
	Canceled    bool
	ErrMsg      string
	Usage       provider.Usage
	// ReasoningRequested 表示客户端显式要求了推理（reasoning_effort / reasoning / thinking）。
	// 只有为 true 时，缺失推理 token 才说明上游掉了思考能力。
	ReasoningRequested bool
	// ReasoningEffort 是客户端本次传的档位原值（空 = 没传）。
	// 与 usage 里的 reasoning_tokens 配对落盘，才能回答「哪一档更省」。
	ReasoningEffort string
	// ReasoningTier 是收敛后的上游实际档位（low/high/max）。
	ReasoningTier string
	Trace         provider.RequestTrace
	Account       accounts.Summary
	AccountID     string

	// SystemForm / SystemFp / SystemDegraded 记录本次请求实际使用的 system 形态。
	// SystemFp 只是 SHA-256 前 8 位，绝不存 system 原文。
	SystemForm     string
	SystemFp       string
	SystemDegraded bool
	// SystemStripped / StripScope 说明这次剥离动到了哪一侧。
	SystemStripped bool
	StripScope     string
	// IO 是进/出两侧的消息统计（nil = 未观测）。
	IO *provider.MessagesIO
	// TitleRewrite 标记本次被改写成了标题生成请求（会话级统计要排除）。
	TitleRewrite bool
	// ErrorCode 是分类后的错误码（空 = 未分类）。
	ErrorCode string
	// UsageSource 区分上游真实值与本地估算（空 → upstream）。
	UsageSource string
}

// RecordChat appends a usage journal row (persisted via usage journal flush).
func (s *Service) RecordChat(input ChatRecordInput) {
	if s == nil {
		return
	}
	duration := time.Since(input.Started).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	s.recordWatchSample(input, duration)
	if s.Journal == nil {
		return
	}
	label := strings.TrimSpace(input.Account.UserName)
	if label == "" {
		label = strings.TrimSpace(input.Account.Label)
	}
	if label == "" {
		label = strings.TrimSpace(input.Account.UserNickname)
	}
	if custom := strings.TrimSpace(input.Account.Label); custom != "" && label != "" && custom != label {
		label = custom + " · " + label
	}
	s.Journal.Record(usagejournal.Entry{
		At:                            input.Started.UnixMilli(),
		ProxyRequestID:                input.ProxyRequestID,
		UpstreamConversationID:        input.Trace.ConversationID,
		UpstreamConversationRequestID: input.Trace.ConversationRequestID,
		UpstreamMessageID:             input.Trace.MessageID,
		SessionKey:                    input.SessionKey,
		SessionLabel:                  input.SessionLabel,
		Model:                         input.Model,
		RoutedModel:                   input.RoutedModel,
		Stream:                        input.Stream,
		Ok:                            input.Ok,
		Error:                         input.ErrMsg,
		AccountID:                     input.AccountID,
		AccountLabel:                  label,
		PromptTokens:                  int64(input.Usage.PromptTokens),
		CompletionTokens:              int64(input.Usage.CompletionTokens),
		CachedTokens:                  int64(input.Usage.CachedTokens()),
		ReasoningTokens:               int64(reasoningTokens(input.Usage)),
		ReasoningEffort:               input.ReasoningEffort,
		ReasoningTier:                 input.ReasoningTier,
		Credit:                        input.Usage.Credit,
		DurationMs:                    duration,
		SystemForm:                    input.SystemForm,
		SystemFp:                      input.SystemFp,
		SystemDegraded:                input.SystemDegraded,
		SystemLen:                     ioSystemLen(input),
		SystemStripped:                input.SystemStripped,
		StripScope:                    input.StripScope,
		ErrorCode:                     input.ErrorCode,
		UsageSource:                   ioUsageSource(input),
		TitleRewrite:                  input.TitleRewrite,
		InMessages:                    ioInt(input.IO, func(m *provider.MessagesIO) int { return m.InMessages }),
		OutMessages:                   ioInt(input.IO, func(m *provider.MessagesIO) int { return m.OutMessages }),
		InBytes:                       ioInt(input.IO, func(m *provider.MessagesIO) int { return m.InBytes }),
		OutBytes:                      ioInt(input.IO, func(m *provider.MessagesIO) int { return m.OutBytes }),
	})
	s.submitInvariantRecord(input)
}

// submitInvariantRecord 把一条请求喂给不变量执行器。
//
// 与 usage journal 分开：journal 是给人看的明细（有容量上限），
// 这里是给断言看的输入（无容量上限，且要带 IO 两侧统计与豁免标记）。
func (s *Service) submitInvariantRecord(input ChatRecordInput) {
	sink := s.InvariantSink()
	if sink == nil {
		return
	}
	rec := decision.Record{
		Kind:             decision.KindRequest,
		At:               input.Started.UnixMilli(),
		RequestID:        input.ProxyRequestID,
		SessionKey:       input.SessionKey,
		AccountID:        input.AccountID,
		Model:            input.Model,
		RoutedModel:      input.RoutedModel,
		PromptTokens:     int64(input.Usage.PromptTokens),
		CompletionTokens: int64(input.Usage.CompletionTokens),
		UsageSource:      input.UsageSource,
		Canceled:         input.Canceled,
		TitleRewrite:     input.TitleRewrite,
		SystemFp:         input.SystemFp,
		SystemForm:       input.SystemForm,
		Stripped:         input.SystemStripped,
		StripScope:       input.StripScope,
		Degraded:         input.SystemDegraded,
		Ok:               input.Ok,
		ErrorCode:        input.ErrorCode,
		ErrorText:        input.ErrMsg,
		Credit:           input.Usage.Credit,
	}
	if input.IO != nil {
		rec.InMessages = input.IO.InMessages
		rec.OutMessages = input.IO.OutMessages
		rec.InBytes = input.IO.InBytes
		rec.OutBytes = input.IO.OutBytes
		rec.InSystemLen = input.IO.InSystemLen
		rec.OutSystemLen = input.IO.OutSystemLen
	}
	sink.Submit(rec)
}

// ioSystemLen 取出站 system 的字节数（未观测时 0）。
func ioSystemLen(input ChatRecordInput) int {
	if input.IO == nil {
		return 0
	}
	return input.IO.OutSystemLen
}

// ioUsageSource 空值按 upstream 处理：老调用方不会填这个字段。
func ioUsageSource(input ChatRecordInput) string {
	if input.UsageSource != "" {
		return input.UsageSource
	}
	return "upstream"
}

func ioInt(io *provider.MessagesIO, pick func(*provider.MessagesIO) int) int {
	if io == nil {
		return 0
	}
	return pick(io)
}

// reasoningTokens 取出 completion_tokens_details.reasoning_tokens（没有则 0）。
func reasoningTokens(u provider.Usage) int {
	if u.CompletionTokensDetails == nil {
		return 0
	}
	return u.CompletionTokensDetails.ReasoningTokens
}

// recordWatchSample 把一次真实请求喂给波动监测器；未启用时是空操作。
func (s *Service) recordWatchSample(input ChatRecordInput, durationMs int64) {
	if s.Watch == nil || input.Canceled {
		return
	}
	var reasoning int64
	if d := input.Usage.CompletionTokensDetails; d != nil {
		reasoning = int64(d.ReasoningTokens)
	}
	s.Watch.Record(watch.Sample{
		At:              input.Started.UnixMilli(),
		Model:           input.Model,
		Ok:              input.Ok,
		ReasoningAsked:  input.ReasoningRequested,
		ReasoningTokens: reasoning,
		DurationMs:      durationMs,
	})
}

// UsageView returns admin usage summary and a page of request rows.
func (s *Service) UsageView(rangeName string, limit, offset int) usagejournal.View {
	return s.UsageQuery(usagejournal.Query{Range: rangeName, Limit: limit, Offset: offset})
}

// UsageQuery returns usage with optional account / model filters.
func (s *Service) UsageQuery(q usagejournal.Query) usagejournal.View {
	if s == nil || s.Journal == nil {
		return usagejournal.View{Summary: usagejournal.Summary{Range: q.Range}, Limit: q.Limit, Offset: q.Offset}
	}
	switch strings.ToLower(strings.TrimSpace(q.Range)) {
	case "week", "month":
		return s.Journal.Query(q)
	default:
		q.Range = "day"
		return s.Journal.Query(q)
	}
}
