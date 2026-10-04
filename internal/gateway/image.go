package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

// ImageFromPool selects an account and calls the upstream image endpoint.
// Retry rules match chat: auth failures refresh the same account once;
// 429/502/503/504 rotate accounts. Image requests are not session-pinned.
func (s *Service) ImageFromPool(ctx context.Context, opts ImageOptions) (ImageResult, error) {
	if opts.RetryDepth >= defaultMaxAccountRetries {
		return ImageResult{}, fmt.Errorf("%w（max=%d）", errRetryDepthExceeded, defaultMaxAccountRetries)
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
		return ImageResult{}, err
	}
	selection, err = s.refreshSelected(ctx, selection, false)
	if err != nil {
		return ImageResult{}, err
	}
	account := selection.Account
	chatOpts := s.chatOptionsFromAccount(account, CompleteOptions{Model: opts.Model, Site: site})
	result, err := s.Provider.Image(ctx, chatOpts, provider.ImageRequest{
		Model:     opts.Model,
		Prompt:    opts.Prompt,
		ImageURLs: opts.ImageURLs,
		ImageData: opts.ImageData,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || strings.Contains(strings.ToLower(err.Error()), "context canceled") {
			s.Log.Info("codebuddy image request canceled by client", "accountId", account.ID, "model", opts.Model)
			return ImageResult{}, err
		}
		complete := CompleteOptions{
			Site:         site,
			Model:        opts.Model,
			ExcludeIDs:   opts.ExcludeIDs,
			RefreshRetry: opts.RefreshRetry,
			RetryDepth:   opts.RetryDepth,
		}
		if s.shouldRetryNextAccount(err, selection, complete) {
			cooldown := s.resolveFailureCooldown(ctx, account, err)
			_ = s.Pool.MarkResult(selection, false, err.Error(), cooldown)
			s.refreshCandidateQuotas(ctx, append(append([]string{}, opts.ExcludeIDs...), account.ID), false, site)
			s.Log.Warn("retrying codebuddy image request with next account", "accountId", account.ID, "error", err.Error())
			opts.ExcludeIDs = append(append([]string{}, opts.ExcludeIDs...), account.ID)
			opts.AccountID = ""
			opts.RetryDepth++
			retried, retryErr := s.ImageFromPool(ctx, opts)
			if retryErr != nil {
				if isRetryExhausted(retryErr) {
					return ImageResult{}, err
				}
				return ImageResult{}, retryErr
			}
			return retried, nil
		}
		if !opts.RefreshRetry && s.shouldRefreshAfterFailure(err, selection) {
			if refreshed, refreshErr := s.refreshSelected(ctx, selection, true); refreshErr == nil && refreshed.Account.BearerToken != account.BearerToken {
				s.Log.Info("retrying codebuddy image request after oauth refresh", "accountId", refreshed.Account.ID)
				opts.AccountID = refreshed.Account.ID
				opts.RefreshRetry = true
				opts.RetryDepth++
				return s.ImageFromPool(ctx, opts)
			}
		}
		_ = s.Pool.MarkResult(selection, false, err.Error(), s.resolveFailureCooldown(ctx, account, err))
		return ImageResult{}, err
	}
	_ = s.Pool.MarkResult(selection, true, "", 0)
	return ImageResult{
		ImageResult: result,
		Account:     accounts.SummarizeAccount(account),
		AccountID:   account.ID,
	}, nil
}
