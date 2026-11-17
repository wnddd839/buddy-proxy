package server

import (
	"context"

	"github.com/wnddd839/codebuddy-proxy/internal/gateway"
)

// completeWithRequestedSearch never enables search by default. A chat or
// Responses handler must pass an explicit client capability declaration.
func (s *Server) completeWithRequestedSearch(
	ctx context.Context,
	opts gateway.CompleteOptions,
	requested bool,
) (gateway.CompleteResult, error) {
	if !requested {
		return s.Svc.CompleteFromPool(ctx, opts)
	}
	searchCtx, cancel := context.WithTimeout(ctx, responsesSearchOverallTimeout)
	defer cancel()
	roundOpts, result, err := s.prepareBuiltinSearch(searchCtx, opts, opts.Site)
	// 调用方（server.go / responses.go 的 chat、stream 路径）按值传入 opts，
	// 却仍用自己那份 opts 去 recordChatJournal；两者共享同一个 SystemOutcome 指针。
	// 搜索每轮用独立 outcome，必须把最终一轮的形态写回该指针，成功与失败路径的
	// 07 用量行才能拿到回填值（而不是空或两轮混合值）。不改调用方签名、
	// 不动 recordChatJournal 本身。
	if opts.SystemOutcome != nil && roundOpts.SystemOutcome != nil {
		*opts.SystemOutcome = *roundOpts.SystemOutcome
	}
	if err != nil {
		return gateway.CompleteResult{}, err
	}
	return *result, nil
}
