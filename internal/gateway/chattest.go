package gateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/models"
	"github.com/wnddd839/codebuddy-proxy/internal/strutil"
)

const (
	chatTestPrompt         = "ping"
	chatTestMaxTokens      = 8
	chatTestRequestTimeout = 20 * time.Second
	chatTestBatchMax       = 5 * time.Minute
	chatTestAccountGap     = 350 * time.Millisecond
)

// AccountChatTestResult is one pinned-account chat probe (read-only; no pool writes).
type AccountChatTestResult struct {
	OK        bool   `json:"ok,omitzero"`
	AccountID string `json:"accountId"`
	Label     string `json:"label,omitempty"`
	Site      string `json:"site"`
	Model     string `json:"model"`
	LatencyMs int64  `json:"latencyMs,omitzero"`
	Message   string `json:"message"`
}

type AccountChatTestSummary struct {
	Total   int `json:"total,omitzero"`
	Passed  int `json:"passed,omitzero"`
	Failed  int `json:"failed,omitzero"`
	Skipped int `json:"skipped,omitzero"`
}

type AccountChatTestBatchResult struct {
	OK       bool                    `json:"ok,omitzero"`
	PoolSite string                  `json:"poolSite"`
	Model    string                  `json:"model"`
	Results  []AccountChatTestResult `json:"results"`
	Summary  AccountChatTestSummary  `json:"summary"`
	Note     string                  `json:"note,omitempty"`
}

// RunPoolChatTestOptions tunes batch behavior for tests; zero values use production defaults.
type RunPoolChatTestOptions struct {
	Gap time.Duration // negative = no gap; zero = chatTestAccountGap
}

// ResolveChatTestModel returns an explicit model, or the lowest credit-multiplier id from the cached catalog.
func (s *Service) ResolveChatTestModel(ctx context.Context, site, model string) (string, error) {
	model = strings.TrimSpace(model)
	if model != "" {
		return model, nil
	}
	listed, err := s.ListModelsForSite(ctx, site, false)
	if err != nil {
		return "", err
	}
	id, ok := models.LowestMultiplierID(listed.Models)
	if !ok {
		return "", fmt.Errorf("no models available for chat test")
	}
	return id, nil
}

// TestAccountChat sends a minimal chat against a pinned account without pool side effects.
func (s *Service) TestAccountChat(ctx context.Context, account accounts.Account, model string) AccountChatTestResult {
	site := config.NormalizeSite(strutil.First(account.Site, s.Config().Site))
	model = strings.TrimSpace(model)
	if model == "" {
		model = "auto"
	}
	out := AccountChatTestResult{
		AccountID: account.ID,
		Label:     account.Label,
		Site:      site,
		Model:     model,
	}
	if !accounts.HasCredentials(account) {
		out.Message = "account has no credentials"
		return out
	}

	reqCtx, cancel := withChatTestTimeout(ctx)
	defer cancel()

	chatOpts := s.chatOptionsFromAccount(account, CompleteOptions{
		Model:               model,
		Messages:            []map[string]any{{"role": "user", "content": chatTestPrompt}},
		MaxCompletionTokens: chatTestMaxTokens,
	})
	started := time.Now()
	_, err := s.Provider.Complete(reqCtx, chatOpts)
	out.LatencyMs = time.Since(started).Milliseconds()
	if err != nil {
		out.Message = strutil.Truncate(err.Error(), 240)
		return out
	}
	out.OK = true
	out.Message = "chat ok"
	return out
}

// RunPoolChatTest probes enabled credentialed accounts for poolSite, serially with a small gap.
func (s *Service) RunPoolChatTest(ctx context.Context, poolSite, model string, opts RunPoolChatTestOptions) AccountChatTestBatchResult {
	poolSite = config.NormalizeSite(poolSite)
	if poolSite == "" {
		poolSite = s.ActivePoolSite()
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "auto"
	}

	store, err := s.Pool.Read()
	out := AccountChatTestBatchResult{
		OK:       true,
		PoolSite: poolSite,
		Model:    model,
	}
	if err != nil {
		out.OK = false
		out.Note = err.Error()
		return out
	}

	eligible := eligibleChatTestAccounts(store, poolSite)
	if len(eligible) == 0 {
		out.Note = siteLabelForChatTest(poolSite) + "号池没有可测试的启用账号。"
		return out
	}

	batchCtx, batchCancel := context.WithTimeout(ctx, chatTestBatchTimeout(len(eligible)))
	defer batchCancel()

	gap := opts.Gap
	if gap == 0 {
		gap = chatTestAccountGap
	}
	if gap < 0 {
		gap = 0
	}

	for i, account := range eligible {
		if batchCtx.Err() != nil {
			out.addUnattempted(account, model, "批次超时，未执行测试")
			out.OK = false
			continue
		}
		if i > 0 && gap > 0 {
			timer := time.NewTimer(gap)
			select {
			case <-batchCtx.Done():
				timer.Stop()
				out.addUnattempted(account, model, "批次超时，未执行测试")
				out.OK = false
				continue
			case <-timer.C:
			}
		}
		reqCtx, cancel := context.WithTimeout(batchCtx, chatTestRequestTimeout)
		item := s.TestAccountChat(reqCtx, account, model)
		cancel()
		out.add(item)
	}
	return out
}

func (b *AccountChatTestBatchResult) add(item AccountChatTestResult) {
	b.Summary.Total++
	b.Results = append(b.Results, item)
	if item.OK {
		b.Summary.Passed++
		return
	}
	b.Summary.Failed++
	b.OK = false
}

func (b *AccountChatTestBatchResult) addUnattempted(account accounts.Account, model, message string) {
	b.Summary.Total++
	b.Summary.Skipped++
	b.Results = append(b.Results, AccountChatTestResult{
		AccountID: account.ID,
		Label:     account.Label,
		Site:      config.NormalizeSite(account.Site),
		Model:     model,
		Message:   message,
	})
}

func eligibleChatTestAccounts(store accounts.Store, poolSite string) []accounts.Account {
	poolSite = config.NormalizeSite(poolSite)
	out := make([]accounts.Account, 0, len(store.Accounts))
	for _, account := range store.Accounts {
		if !account.Enabled || !accounts.HasCredentials(account) {
			continue
		}
		if config.NormalizeSite(account.Site) != poolSite {
			continue
		}
		out = append(out, account)
	}
	return out
}

func chatTestBatchTimeout(accountCount int) time.Duration {
	if accountCount <= 0 {
		return 0
	}
	return min(chatTestRequestTimeout*time.Duration(accountCount), chatTestBatchMax)
}

func withChatTestTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, chatTestRequestTimeout)
}

func siteLabelForChatTest(site string) string {
	if config.NormalizeSite(site) == "global" {
		return "国际"
	}
	return "国内"
}
