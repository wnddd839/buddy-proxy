package gateway

import (
	"strings"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
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
	Stream         bool
	Ok             bool
	ErrMsg         string
	Usage          provider.Usage
	// ReasoningRequested 表示客户端显式要求了推理（reasoning_effort / reasoning / thinking）。
	// 只有为 true 时，缺失推理 token 才说明上游掉了思考能力。
	ReasoningRequested bool
	Trace              provider.RequestTrace
	Account            accounts.Summary
	AccountID          string
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
		Stream:                        input.Stream,
		Ok:                            input.Ok,
		Error:                         input.ErrMsg,
		AccountID:                     input.AccountID,
		AccountLabel:                  label,
		PromptTokens:                  int64(input.Usage.PromptTokens),
		CompletionTokens:              int64(input.Usage.CompletionTokens),
		CachedTokens:                  int64(input.Usage.CachedTokens()),
		Credit:                        input.Usage.Credit,
		DurationMs:                    duration,
	})
}

// recordWatchSample 把一次真实请求喂给波动监测器；未启用时是空操作。
func (s *Service) recordWatchSample(input ChatRecordInput, durationMs int64) {
	if s.Watch == nil {
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
