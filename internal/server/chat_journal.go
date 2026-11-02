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
	}
	if result != nil {
		input.Trace = result.Trace
		input.Account = result.Account
		input.AccountID = result.AccountID
	}
	s.Svc.RecordChat(input)
}
