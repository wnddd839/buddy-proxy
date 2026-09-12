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
		ErrMsg:         strings.TrimSpace(errMsg),
		Usage:          usage,
	}
	if result != nil {
		input.Trace = result.Trace
		input.Account = result.Account
		input.AccountID = result.AccountID
	}
	s.Svc.RecordChat(input)
}
