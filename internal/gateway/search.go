package gateway

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

// SearchOptions selects an account for a search / webfetch upstream call.
// Search calls do not pin sessions and do not reuse X-Conversation-ID, matching
// the image path: they can safely retry on another account.
type SearchOptions struct {
	Site         string
	AccountID    string
	FixedAccount bool
	ExcludeIDs   []string
	RefreshRetry bool
	RetryDepth   int
}

// SearchResult is one pooled search / webfetch response plus the serving account.
type SearchResult struct {
	Account   accounts.Summary `json:"account"`
	AccountID string           `json:"accountId"`
}

// AgenticSearchOptions selects an account for the agentic search stream.
type AgenticSearchOptions struct {
	SearchOptions
	Query   string
	OnEvent func(provider.AgenticSearchEvent)
}

// AgenticSearchResult is one pooled agentic search answer.
type AgenticSearchResult struct {
	SearchResult
	Content string
	Events  []provider.AgenticSearchEvent
}

// WebSearchFromPool selects an account and runs one keyword search.
func (s *Service) WebSearchFromPool(ctx context.Context, opts SearchOptions, req provider.WebSearchRequest) (provider.WebSearchResponse, SearchResult, error) {
	var (
		response provider.WebSearchResponse
		meta     SearchResult
	)
	err := s.withSearchAccount(ctx, opts, "search", func(ctx context.Context, account accounts.Account, chatOpts provider.ChatOptions) error {
		result, err := s.Provider.WebSearch(ctx, chatOpts, req)
		if err != nil {
			return err
		}
		response = result
		meta = SearchResult{Account: accounts.SummarizeAccount(account), AccountID: account.ID}
		return nil
	})
	if err != nil {
		return provider.WebSearchResponse{}, SearchResult{}, err
	}
	return response, meta, nil
}

// WebFetchFromPool selects an account and runs one page fetch.
func (s *Service) WebFetchFromPool(ctx context.Context, opts SearchOptions, req provider.WebFetchRequest) (provider.WebFetchResponse, SearchResult, error) {
	var (
		response provider.WebFetchResponse
		meta     SearchResult
	)
	err := s.withSearchAccount(ctx, opts, "webfetch", func(ctx context.Context, account accounts.Account, chatOpts provider.ChatOptions) error {
		result, err := s.Provider.WebFetch(ctx, chatOpts, req)
		if err != nil {
			return err
		}
		response = result
		meta = SearchResult{Account: accounts.SummarizeAccount(account), AccountID: account.ID}
		return nil
	})
	if err != nil {
		return provider.WebFetchResponse{}, SearchResult{}, err
	}
	return response, meta, nil
}

// AgenticSearchFromPool selects an account and runs one multi-step search.
func (s *Service) AgenticSearchFromPool(ctx context.Context, opts AgenticSearchOptions) (AgenticSearchResult, error) {
	var (
		response provider.AgenticSearchResult
		meta     SearchResult
	)
	err := s.withSearchAccount(ctx, opts.SearchOptions, "agentic search", func(ctx context.Context, account accounts.Account, chatOpts provider.ChatOptions) error {
		result, err := s.Provider.AgenticSearch(ctx, chatOpts, opts.Query, opts.OnEvent)
		if err != nil {
			return err
		}
		response = result
		meta = SearchResult{Account: accounts.SummarizeAccount(account), AccountID: account.ID}
		return nil
	})
	if err != nil {
		return AgenticSearchResult{}, err
	}
	return AgenticSearchResult{
		SearchResult: meta,
		Content:      response.Content,
		Events:       response.Events,
	}, nil
}

// withSearchAccount runs one call against a selected account, applying the same
// OAuth refresh and next-account retry policy as chat / images.
func (s *Service) withSearchAccount(
	ctx context.Context,
	opts SearchOptions,
	label string,
	call func(context.Context, accounts.Account, provider.ChatOptions) error,
) error {
	if opts.FixedAccount && opts.AccountID == "" {
		return errors.New("固定账号请求缺少账号约束")
	}
	if opts.RetryDepth >= defaultMaxAccountRetries {
		return fmt.Errorf("%w（max=%d）", errRetryDepthExceeded, defaultMaxAccountRetries)
	}
	site := s.requestSite(CompleteOptions{Site: opts.Site})
	if opts.AccountID == "" && opts.RetryDepth == 0 && s.needsQuotaRefresh(opts.ExcludeIDs, site) {
		s.refreshCandidateQuotas(ctx, opts.ExcludeIDs, true, site)
	}
	selection, err := s.Pool.Select(accounts.SelectOptions{
		AccountID:   opts.AccountID,
		Site:        site,
		ExcludeIDs:  opts.ExcludeIDs,
		PreferQuota: opts.AccountID == "",
	})
	if err != nil {
		return err
	}
	selection, err = s.refreshSelected(ctx, selection, false)
	if err != nil {
		return err
	}
	account := selection.Account
	chatOpts := s.chatOptionsFromAccount(account, CompleteOptions{Site: site})
	endFlight := s.Concurrency.Begin(account.ID, account.Label)
	callErr := call(ctx, account, chatOpts)
	if callErr != nil {
		s.recordRateLimit(account, callErr)
	}
	endFlight(callErr == nil)
	if err := callErr; err != nil {
		if errors.Is(err, context.Canceled) || strings.Contains(strings.ToLower(err.Error()), "context canceled") {
			s.Log.Info("codebuddy "+label+" request canceled by client", "accountId", account.ID)
			return err
		}
		complete := CompleteOptions{
			FixedAccount: opts.FixedAccount,
			Site:         site,
			ExcludeIDs:   opts.ExcludeIDs,
			RefreshRetry: opts.RefreshRetry,
			RetryDepth:   opts.RetryDepth,
		}
		if s.shouldRetryNextAccount(err, selection, complete) {
			cooldown := s.resolveFailureCooldown(ctx, account, err)
			_ = s.Pool.MarkResult(selection, false, err.Error(), cooldown)
			s.refreshCandidateQuotas(ctx, append(slices.Clone(opts.ExcludeIDs), account.ID), false, site)
			s.Log.Warn("retrying codebuddy "+label+" request with next account", "accountId", account.ID, "error", err.Error())
			opts.ExcludeIDs = append(slices.Clone(opts.ExcludeIDs), account.ID)
			opts.AccountID = ""
			opts.RetryDepth++
			retryErr := s.withSearchAccount(ctx, opts, label, call)
			if isRetryExhausted(retryErr) {
				return err
			}
			return retryErr
		}
		if !opts.RefreshRetry && s.shouldRefreshAfterFailure(err, selection) {
			if refreshed, refreshErr := s.refreshSelected(ctx, selection, true); refreshErr == nil && refreshed.Account.BearerToken != account.BearerToken {
				s.Log.Info("retrying codebuddy "+label+" request after oauth refresh", "accountId", refreshed.Account.ID)
				opts.AccountID = refreshed.Account.ID
				opts.RefreshRetry = true
				opts.RetryDepth++
				return s.withSearchAccount(ctx, opts, label, call)
			}
		}
		_ = s.Pool.MarkResult(selection, false, err.Error(), s.resolveFailureCooldown(ctx, account, err))
		return err
	}
	_ = s.Pool.MarkResult(selection, true, "", 0)
	return nil
}
