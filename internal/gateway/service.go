package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/billing"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/models"
	"github.com/wnddd839/codebuddy-proxy/internal/oauth"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/sessionpin"
	"github.com/wnddd839/codebuddy-proxy/internal/strutil"
	"github.com/wnddd839/codebuddy-proxy/internal/usagejournal"
	"github.com/wnddd839/codebuddy-proxy/internal/version"
)

const (
	modelsCacheTTL = 60 * time.Second
	// defaultMaxAccountRetries 单请求最多尝试的账号数（含换号重试），防止无限递归。
	defaultMaxAccountRetries = 16
	// quotaRefreshTimeout 换号/补快照时并行拉候选额度的上限，避免 billing 拖垮这次 chat。
	quotaRefreshTimeout = 2500 * time.Millisecond
	// quotaSnapshotTTL 新会话只刷新过期或缺失的额度快照；同会话钉住后不打 billing。
	quotaSnapshotTTL = 5 * time.Minute
)

var (
	errRetryDepthExceeded = errors.New("CodeBuddy 请求重试深度超限")
	// 包级预编译，避免热路径（每个 chat 请求）重复编译正则。
	reProviderModel    = regexp.MustCompile(`(?i)^codebuddy(?:(?:/|:)(.*))?$`)
	reAuthFailure      = regexp.MustCompile(`(?i)(?:\b401\b|\b403\b|unauthori[sz]ed|forbidden|token|credential|auth|login|not authenticated|未登录|登录|凭证)`)
	reRetryNextAccount = regexp.MustCompile(`(?i)\b429\b|\b502\b|\b503\b|\b504\b|rate.?limit|too many requests|site mismatch|invalid request`)
)

type Stats struct {
	TotalRequests         int64  `json:"totalRequests"`
	SuccessRequests       int64  `json:"successRequests"`
	FailedRequests        int64  `json:"failedRequests"`
	ActiveRequests        int64  `json:"activeRequests"`
	TotalDurationMs       int64  `json:"totalDurationMs"`
	TotalPromptTokens     int64  `json:"totalPromptTokens"`
	TotalCompletionTokens int64  `json:"totalCompletionTokens"`
	TotalTokens           int64  `json:"totalTokens"`
	TotalCachedTokens     int64  `json:"totalCachedTokens"`
	LastRequestAt         int64  `json:"lastRequestAt"`
	LastDurationMs        int64  `json:"lastDurationMs"`
	LastModel             string `json:"lastModel"`
	LastPromptChars       int64  `json:"lastPromptChars"`
	LastOutputChars       int64  `json:"lastOutputChars"`
	LastPromptTokens      int64  `json:"lastPromptTokens"`
	LastCompletionTokens  int64  `json:"lastCompletionTokens"`
	LastCachedTokens      int64  `json:"lastCachedTokens"`
	LastStream            bool   `json:"lastStream"`
	LastError             string `json:"lastError"`
	LastUpstreamBytes     int64  `json:"lastUpstreamBytes"`
	LastEventCount        int64  `json:"lastEventCount"`
	LastDeltaCount        int64  `json:"lastDeltaCount"`
}

type OAuthSession struct {
	ID          string `json:"id"`
	Token       string `json:"token"`
	Status      string `json:"status"`
	Site        string `json:"site"`
	Label       string `json:"label"`
	AuthState   string `json:"authState"`
	URL         string `json:"url"`
	LaunchURL   string `json:"launchUrl"`
	AccessURL   string `json:"accessUrl"`
	CallbackURL string `json:"callbackUrl"`
	Error       string `json:"error"`
	StartedAt   int64  `json:"startedAt"`
	UpdatedAt   int64  `json:"updatedAt"`
	ConfirmedAt int64  `json:"confirmedAt,omitempty"`
}

type Service struct {
	Pool     *accounts.Pool
	Provider *provider.Client
	OAuth    *oauth.Client
	Models   *models.Lister
	Log      *slog.Logger
	Started  time.Time

	runtimeCfg atomic.Pointer[config.Config]

	mu    sync.Mutex
	stats Stats
	oauth *OAuthSession

	modelsCacheMu sync.Mutex
	modelsCache   struct {
		key       string
		result    models.ListResult
		expiresAt time.Time
	}
	modelsFlight modelsFlight
	Pins         *sessionpin.Table
	Journal      *usagejournal.Journal
}

func New(cfg config.Config, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	p := provider.NewClient(cfg)
	p.Log = logger
	journal, journalErr := usagejournal.Open(cfg.UsagePath, 0)
	if journalErr != nil {
		logger.Warn("usage journal load failed; running without persistence", "path", cfg.UsagePath, "error", journalErr.Error())
		journal = usagejournal.New(0)
	} else if journal != nil && cfg.UsagePath != "" {
		logger.Info("usage journal persistence enabled", "path", cfg.UsagePath)
	}
	svc := &Service{
		Pool:     accounts.NewPool(cfg.AccountsPath),
		Provider: p,
		OAuth:    oauth.NewClient(p.HTTP),
		Models:   models.NewLister(),
		Log:      logger,
		Started:  time.Now(),
		oauth:    &OAuthSession{Status: "idle"},
		Pins:     sessionpin.New(0),
		Journal:  journal,
	}
	svc.storeConfig(cfg)
	return svc
}

func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	var err error
	if s.Journal != nil {
		err = s.Journal.Close()
	}
	if s.Pool != nil {
		if poolErr := s.Pool.Close(); poolErr != nil && err == nil {
			err = poolErr
		}
	}
	return err
}

// Config 返回运行时配置快照，并发读安全。
func (s *Service) Config() config.Config {
	if p := s.runtimeCfg.Load(); p != nil {
		return *p
	}
	return config.Config{}
}

func (s *Service) storeConfig(next config.Config) {
	s.runtimeCfg.Store(&next)
}

func (s *Service) updateConfig(mutate func(*config.Config)) config.Config {
	for {
		old := s.runtimeCfg.Load()
		var next config.Config
		if old != nil {
			next = *old
		}
		mutate(&next)
		if s.runtimeCfg.CompareAndSwap(old, &next) {
			return next
		}
	}
}

// SetAPIKey 轮换网关 API Key，并强制开启 requireApiKey。
func (s *Service) SetAPIKey(key string) config.Config {
	return s.updateConfig(func(c *config.Config) {
		c.APIKey = key
		c.RequireAPIKey = true
	})
}

type ProviderModel struct {
	Provider    string
	Model       string
	PublicModel string
}

func ResolveProviderModel(model string) ProviderModel {
	cleaned := strings.TrimSpace(model)
	if match := reProviderModel.FindStringSubmatch(cleaned); match != nil {
		requested := strings.TrimSpace(match[1])
		if requested == "" || requested == "default" {
			requested = "auto"
		}
		return ProviderModel{Provider: "codebuddy", Model: requested, PublicModel: models.PublicModelID(requested)}
	}
	if cleaned == "" {
		return ProviderModel{Provider: "codebuddy", Model: "auto", PublicModel: "auto"}
	}
	publicID := models.PublicModelID(cleaned)
	return ProviderModel{Provider: "codebuddy", Model: publicID, PublicModel: publicID}
}

type CompleteOptions struct {
	AccountID           string
	SessionKey          string
	SessionLabel        string
	Model               string
	Messages            []map[string]any
	Stream              bool
	Tools               any
	ToolChoice          any
	Temperature         *float64
	TopP                *float64
	MaxCompletionTokens int
	ReasoningEffort     string
	Reasoning           map[string]any
	Thinking            map[string]any
	ExcludeIDs          []string
	OnDelta             func(string)
	OnThinkingDelta     func(string)
	OnEvent             func(provider.Event)
	RefreshRetry        bool
	RetryDepth          int // 内部递归计数，调用方勿手动设置
}

// chatOptionsFromAccount 以账号为区域真源构建上游请求。
// 反代进程所在位置 / 全局 CODEBUDDY_BASE_URL 不得把国内账号打到海外（反之亦然）。
func (s *Service) chatOptionsFromAccount(account accounts.Account, opts CompleteOptions) provider.ChatOptions {
	site := config.NormalizeSite(strutil.First(account.Site, s.Config().Site))
	product := config.NormalizeProduct(s.Config().Product)
	internet := strutil.First(account.InternetEnvironment, s.Config().InternetEnvironment)
	baseURL := strings.TrimSpace(account.BaseURL)
	if product == "workbuddy" || baseURL == "" {
		baseURL = config.ProductPortalBaseURL(site, product)
	}
	return provider.ChatOptions{
		Model:               opts.Model,
		Messages:            opts.Messages,
		Stream:              opts.Stream,
		Tools:               opts.Tools,
		ToolChoice:          opts.ToolChoice,
		Temperature:         opts.Temperature,
		TopP:                opts.TopP,
		MaxCompletionTokens: opts.MaxCompletionTokens,
		ReasoningEffort:     opts.ReasoningEffort,
		Reasoning:           opts.Reasoning,
		Thinking:            opts.Thinking,
		BearerToken:         account.BearerToken,
		UserID:              account.AuthStatus.UserID,
		BaseURL:             baseURL,
		Site:                site,
		Product:             product,
		InternetEnvironment: internet,
		// 仅使用账号级 APIEndpoint，进程级 endpoint 可能指向错误区域。
		// 切到 WorkBuddy 时丢掉 codebuddy/copilot 上的账号级 endpoint，否则请求仍打旧域。
		APIEndpoint:         provider.AlignAPIEndpoint(product, account.APIEndpoint),
		ChatCompletionsPath: strutil.First(account.ChatCompletionsPath, s.Config().ChatCompletionsPath),
		// 不传 Domain：X-Domain 由 chat endpoint 主机名推导。
		EnterpriseID:       account.EnterpriseID,
		TenantID:           account.TenantID,
		DepartmentFullName: account.DepartmentFullName,
		OnDelta:            opts.OnDelta,
		OnThinkingDelta:    opts.OnThinkingDelta,
		OnEvent:            opts.OnEvent,
	}
}

type CompleteResult struct {
	provider.Result
	Account   accounts.Summary `json:"account"`
	AccountID string           `json:"accountId"`
}

func (s *Service) CompleteFromPool(ctx context.Context, opts CompleteOptions) (CompleteResult, error) {
	if opts.RetryDepth >= defaultMaxAccountRetries {
		return CompleteResult{}, fmt.Errorf("%w（max=%d）", errRetryDepthExceeded, defaultMaxAccountRetries)
	}
	if opts.SessionKey == "" {
		opts.SessionKey = sessionpin.Key(nil, "", opts.Messages)
	}
	if opts.AccountID == "" && opts.SessionKey != "" {
		if id, ok := s.Pins.Lookup(opts.SessionKey); ok && !slices.Contains(opts.ExcludeIDs, id) {
			opts.AccountID = id
		} else if ok {
			s.Pins.Forget(opts.SessionKey)
		}
	}
	if opts.AccountID == "" && opts.RetryDepth == 0 && s.needsQuotaRefresh(opts.ExcludeIDs) {
		s.refreshCandidateQuotas(ctx, opts.ExcludeIDs, true)
	}
	selection, err := s.Pool.Select(accounts.SelectOptions{
		AccountID:   opts.AccountID,
		Site:        s.ActivePoolSite(),
		ExcludeIDs:  opts.ExcludeIDs,
		PreferQuota: opts.AccountID == "",
	})
	if err != nil {
		return CompleteResult{}, err
	}
	selection, err = s.refreshSelected(ctx, selection, false)
	if err != nil {
		return CompleteResult{}, err
	}
	account := selection.Account
	if selection.BypassedCooldown {
		s.Log.Debug("pool select bypassed cooldown (all accounts cooling)", "accountId", account.ID, "cooldownUntil", account.CooldownUntil)
	}
	chatOpts := s.chatOptionsFromAccount(account, opts)
	result, err := s.Provider.Complete(ctx, chatOpts)
	if err != nil {
		// 客户端主动断开 SSE/HTTP，不算账号或上游故障。
		if errors.Is(err, context.Canceled) || strings.Contains(strings.ToLower(err.Error()), "context canceled") {
			s.Log.Info("codebuddy request canceled by client", "accountId", account.ID, "model", opts.Model)
			return CompleteResult{}, err
		}
		if s.shouldRetryNextAccount(err, selection, opts) {
			if opts.SessionKey != "" {
				s.Pins.Forget(opts.SessionKey)
			}
			cooldown := s.resolveFailureCooldown(ctx, account, err)
			_ = s.Pool.MarkResult(selection, false, err.Error(), cooldown)
			s.refreshCandidateQuotas(ctx, append(append([]string{}, opts.ExcludeIDs...), account.ID), false)
			s.Log.Warn("retrying codebuddy request with next account", "accountId", account.ID, "error", err.Error())
			next := append(append([]string{}, opts.ExcludeIDs...), account.ID)
			opts.ExcludeIDs = next
			opts.AccountID = ""
			opts.RetryDepth++
			retried, retryErr := s.CompleteFromPool(ctx, opts)
			if retryErr != nil {
				// 不要用「无可用账号 / 重试深度超限」掩盖真实上游错误（429/503 等）。
				if isRetryExhausted(retryErr) {
					return CompleteResult{}, err
				}
				return CompleteResult{}, retryErr
			}
			return retried, nil
		}
		if !opts.RefreshRetry && s.shouldRefreshAfterFailure(err, selection) {
			if refreshed, refreshErr := s.refreshSelected(ctx, selection, true); refreshErr == nil && refreshed.Account.BearerToken != account.BearerToken {
				s.Log.Info("retrying codebuddy request after oauth refresh", "accountId", refreshed.Account.ID)
				opts.AccountID = refreshed.Account.ID
				opts.RefreshRetry = true
				opts.RetryDepth++
				return s.CompleteFromPool(ctx, opts)
			}
		}
		_ = s.Pool.MarkResult(selection, false, err.Error(), s.resolveFailureCooldown(ctx, account, err))
		return CompleteResult{}, err
	}
	_ = s.Pool.MarkResult(selection, true, "", 0)
	if opts.SessionKey != "" {
		s.Pins.Remember(opts.SessionKey, account.ID)
	}
	return CompleteResult{
		Result:    result,
		Account:   accounts.SummarizeAccount(account),
		AccountID: account.ID,
	}, nil
}

func (s *Service) refreshSelected(ctx context.Context, selection accounts.Selection, force bool) (accounts.Selection, error) {
	account := selection.Account
	if !oauth.ShouldRefresh(account, force, s.Config().RefreshWindow, time.Now()) {
		return selection, nil
	}
	token, err := s.OAuth.Refresh(ctx, oauth.RefreshOptions{
		Site:         strutil.First(account.Site, s.Config().Site),
		BaseURL:      strutil.First(account.BaseURL, s.Config().BaseURL),
		AccessToken:  account.BearerToken,
		RefreshToken: account.RefreshToken,
	})
	if err != nil {
		if force || (account.TokenExpiresAt > 0 && account.TokenExpiresAt <= time.Now().UnixMilli()) {
			return selection, fmt.Errorf("CodeBuddy OAuth refresh failed; please re-authenticate from /direct-admin/#codebuddy. %w", err)
		}
		s.Log.Warn("codebuddy oauth refresh skipped after failure", "accountId", account.ID, "error", err.Error())
		return selection, nil
	}
	updated := oauth.AccountFromTokenData(token, strutil.First(account.Site, s.Config().Site), account.Label)
	updated.ID = account.ID
	updated.Enabled = account.Enabled
	updated.BaseURL = account.BaseURL
	updated.InternetEnvironment = account.InternetEnvironment
	updated.APIEndpoint = account.APIEndpoint
	updated.ChatCompletionsPath = account.ChatCompletionsPath
	updated.Transport = account.Transport
	updated.SuccessRequests = account.SuccessRequests
	updated.FailedRequests = account.FailedRequests
	updated.LastUsedAt = account.LastUsedAt
	updated.LastSelectedAt = account.LastSelectedAt
	updated.CreatedAt = account.CreatedAt
	updated.CooldownUntil = account.CooldownUntil
	updated.QuotaRemaining = account.QuotaRemaining
	updated.QuotaUnlimited = account.QuotaUnlimited
	updated.QuotaResetAt = account.QuotaResetAt
	updated.QuotaCheckedAt = account.QuotaCheckedAt
	saved, _, err := s.Pool.ReplaceAccount(updated)
	if err != nil {
		return selection, err
	}
	selection.Account = saved
	s.Log.Info("codebuddy oauth credential refreshed", "accountId", saved.ID, "site", saved.Site)
	return selection, nil
}

func (s *Service) shouldRefreshAfterFailure(err error, selection accounts.Selection) bool {
	if strings.TrimSpace(selection.Account.RefreshToken) == "" {
		return false
	}
	msg := strings.ToLower(err.Error())
	// 策略 / 请求形态错误，刷新凭证也救不了。
	if strings.Contains(msg, "11140") || strings.Contains(msg, "request illegal") ||
		strings.Contains(msg, "11128") || strings.Contains(msg, "unapproved channel") ||
		strings.Contains(msg, "11101") || strings.Contains(msg, "11102") {
		return false
	}
	return reAuthFailure.MatchString(msg)
}

func (s *Service) shouldRetryNextAccount(err error, selection accounts.Selection, opts CompleteOptions) bool {
	if selection.Account.ID == "" {
		return false
	}
	if slices.Contains(opts.ExcludeIDs, selection.Account.ID) {
		return false
	}
	if s.shouldRefreshAfterFailure(err, selection) {
		return false
	}
	msg := strings.ToLower(err.Error())
	// 11128 为渠道/策略限制，换号重试通常无效（客户端指纹相同）。
	if strings.Contains(msg, "11128") || strings.Contains(msg, "unapproved channel") {
		return false
	}
	// 11101/11102 为请求形态或模型不可用，换号无意义。
	if strings.Contains(msg, "11101") || strings.Contains(msg, "11102") {
		return false
	}
	// 11140 为区域/策略非法请求，换号通常无效。
	if strings.Contains(msg, "11140") || strings.Contains(msg, "request illegal") {
		return false
	}
	if billing.IsQuotaExhaustedError(err) {
		return true
	}
	return reRetryNextAccount.MatchString(err.Error())
}

func failureCooldown(err error) time.Duration {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "11140"), strings.Contains(msg, "request illegal"),
		strings.Contains(msg, "11128"), strings.Contains(msg, "unapproved channel"),
		strings.Contains(msg, "11101"), strings.Contains(msg, "11102"):
		return 5 * time.Minute
	case strings.Contains(msg, "502"), strings.Contains(msg, "503"), strings.Contains(msg, "504"):
		return 30 * time.Second
	case strings.Contains(msg, "429"), strings.Contains(msg, "rate limit"), strings.Contains(msg, "too many requests"):
		return 2 * time.Minute
	default:
		return 0
	}
}

func isRetryExhausted(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, accounts.ErrNoAccounts) || errors.Is(err, errRetryDepthExceeded) {
		return true
	}
	return strings.Contains(err.Error(), "no enabled CodeBuddy accounts")
}

func (s *Service) resolveFailureCooldown(ctx context.Context, account accounts.Account, err error) time.Duration {
	base := failureCooldown(err)
	if s == nil || s.Pool == nil || s.Provider == nil || !billing.IsQuotaExhaustedError(err) {
		return base
	}
	usage, probeErr := billing.FetchAccountUsage(ctx, s.Provider, account, s.Config())
	if probeErr != nil {
		s.Log.Debug("quota probe skipped after upstream failure", "accountId", account.ID, "error", probeErr.Error())
		return base
	}
	now := time.Now()
	state := billing.QuotaStateFromUsage(usage, now)
	if _, applyErr := s.Pool.ApplyQuotaState(account.ID, state.Remaining, state.Unlimited, state.ResetAt, state.CheckedAt); applyErr != nil {
		s.Log.Warn("quota state apply failed after probe", "accountId", account.ID, "error", applyErr.Error())
	}
	if quotaCooldown := state.CooldownDuration(now); quotaCooldown > base {
		s.Log.Info("account quota cooldown aligned with upstream reset",
			"accountId", account.ID,
			"resetAt", state.ResetAt,
			"cooldown", quotaCooldown.String(),
		)
		return quotaCooldown
	}
	return base
}

func quotaSnapshotFresh(account accounts.Account, nowMillis int64) bool {
	if account.QuotaUnlimited {
		return true
	}
	if account.QuotaRemaining == nil || account.QuotaCheckedAt <= 0 {
		return false
	}
	return nowMillis-account.QuotaCheckedAt < quotaSnapshotTTL.Milliseconds()
}

func (s *Service) listSelectableAccounts(exclude []string) []accounts.Account {
	if s == nil || s.Pool == nil {
		return nil
	}
	store, err := s.Pool.Read()
	if err != nil {
		return nil
	}
	site := s.ActivePoolSite()
	now := time.Now().UnixMilli()
	skip := map[string]struct{}{}
	for _, id := range exclude {
		if id != "" {
			skip[id] = struct{}{}
		}
	}
	out := make([]accounts.Account, 0, len(store.Accounts))
	for _, account := range store.Accounts {
		if !account.Enabled || !accounts.HasCredentials(account) {
			continue
		}
		if site != "" && config.NormalizeSite(account.Site) != site {
			continue
		}
		if _, ok := skip[account.ID]; ok {
			continue
		}
		if accounts.InCooldown(account, now) || accounts.QuotaBlocked(account, now) {
			continue
		}
		out = append(out, account)
	}
	return out
}

// needsQuotaRefresh 任一可选账号缺快照或超过 TTL，新会话就要补探，避免只查过部分号时 PreferQuota 看不见更厚的号。
func (s *Service) needsQuotaRefresh(exclude []string) bool {
	now := time.Now().UnixMilli()
	for _, account := range s.listSelectableAccounts(exclude) {
		if !quotaSnapshotFresh(account, now) {
			return true
		}
	}
	return false
}

// refreshCandidateQuotas 并行刷新候选额度。staleOnly 时只打缺失/过期的号（新会话）；换号时刷全部活号。
func (s *Service) refreshCandidateQuotas(ctx context.Context, exclude []string, staleOnly bool) {
	if s == nil || s.Pool == nil || s.Provider == nil {
		return
	}
	now := time.Now().UnixMilli()
	candidates := s.listSelectableAccounts(exclude)
	if staleOnly {
		filtered := candidates[:0]
		for _, account := range candidates {
			if !quotaSnapshotFresh(account, now) {
				filtered = append(filtered, account)
			}
		}
		candidates = filtered
	}
	if len(candidates) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, quotaRefreshTimeout)
	defer cancel()
	started := time.Now()
	var wg sync.WaitGroup
	var refreshed atomic.Int32
	for _, account := range candidates {
		wg.Go(func() {
			usage, probeErr := billing.FetchAccountCredits(ctx, s.Provider, account, s.Config())
			if probeErr != nil {
				s.Log.Debug("candidate quota refresh skipped", "accountId", account.ID, "error", probeErr.Error())
				return
			}
			if !usage.Credits.Unlimited && len(usage.Credits.Packages) == 0 {
				return
			}
			state := billing.QuotaStateFromUsage(usage, time.Now())
			if _, applyErr := s.Pool.ApplyQuotaState(account.ID, state.Remaining, state.Unlimited, state.ResetAt, state.CheckedAt); applyErr != nil {
				s.Log.Debug("candidate quota apply failed", "accountId", account.ID, "error", applyErr.Error())
				return
			}
			refreshed.Add(1)
		})
	}
	wg.Wait()
	s.Log.Info("refreshed candidate quotas",
		"candidates", len(candidates),
		"updated", refreshed.Load(),
		"staleOnly", staleOnly,
		"elapsed", time.Since(started).String(),
	)
}

func (s *Service) ListModels(ctx context.Context, fresh bool) (models.ListResult, error) {
	store, err := s.Pool.Read()
	if err != nil {
		return models.ListResult{}, err
	}
	activeSite := s.ActivePoolSite()
	summary := accounts.SummarizeStoreForSite(store, s.Pool.Path(), activeSite)
	if summary.Primary == nil || !summary.Primary.HasCredentials || config.NormalizeSite(summary.Primary.Site) != activeSite {
		return models.ListResult{
			OK:           false,
			Site:         activeSite,
			Models:       models.ToAdminModels([]map[string]any{{"id": "auto", "name": "Auto"}}, "fallback"),
			ModelsSource: "no_credentials",
			Message:      "当前号池区域(" + activeSite + ")没有可用账号，请先 OAuth 登录该区域或切换号池。",
		}, nil
	}
	var account accounts.Account
	for _, item := range store.Accounts {
		if item.ID == summary.Primary.ID {
			account = item
			break
		}
	}
	site := config.NormalizeSite(strutil.First(account.Site, s.Config().Site))
	product := config.NormalizeProduct(s.Config().Product)
	internet := strutil.First(account.InternetEnvironment, s.Config().InternetEnvironment)
	baseURL := strings.TrimSpace(account.BaseURL)
	if product == "workbuddy" || baseURL == "" {
		baseURL = config.ProductPortalBaseURL(site, product)
	}
	cacheKey := strings.Join([]string{activeSite, product, account.ID, site, baseURL, internet}, "|")
	if !fresh {
		s.modelsCacheMu.Lock()
		hit := s.modelsCache.key == cacheKey && time.Now().Before(s.modelsCache.expiresAt)
		cached := s.modelsCache.result
		s.modelsCacheMu.Unlock()
		if hit && len(cached.Models) > 0 {
			return cached, nil
		}
	}

	listed, err, _ := s.modelsFlight.Do(cacheKey, func() (models.ListResult, error) {
		if !fresh {
			s.modelsCacheMu.Lock()
			hit := s.modelsCache.key == cacheKey && time.Now().Before(s.modelsCache.expiresAt)
			cached := s.modelsCache.result
			s.modelsCacheMu.Unlock()
			if hit && len(cached.Models) > 0 {
				return cached, nil
			}
		}
		out := s.Models.List(ctx, s.Provider, models.ListOptions{
			Site:                site,
			Product:             product,
			BaseURL:             baseURL,
			InternetEnvironment: internet,
			BearerToken:         account.BearerToken,
			UserID:              account.AuthStatus.UserID,
			EnterpriseID:        account.EnterpriseID,
			TenantID:            account.TenantID,
			DepartmentFullName:  account.DepartmentFullName,
			APIEndpoint:         provider.AlignAPIEndpoint(product, account.APIEndpoint),
			ChatCompletionsPath: strutil.First(account.ChatCompletionsPath, s.Config().ChatCompletionsPath),
		})
		if out.OK && len(out.Models) > 0 {
			s.modelsCacheMu.Lock()
			s.modelsCache.key = cacheKey
			s.modelsCache.result = out
			s.modelsCache.expiresAt = time.Now().Add(modelsCacheTTL)
			s.modelsCacheMu.Unlock()
		}
		return out, nil
	})
	return listed, err
}

func (s *Service) invalidateModelsCache() {
	s.modelsCacheMu.Lock()
	s.modelsCache.key = ""
	s.modelsCache.result = models.ListResult{}
	s.modelsCache.expiresAt = time.Time{}
	s.modelsCacheMu.Unlock()
}

func (s *Service) ConfiguredModels() []models.Model {
	out := make([]models.Model, 0, len(s.Config().DefaultModels))
	for _, id := range s.Config().DefaultModels {
		out = append(out, models.Model{
			ID: models.PublicModelID(id), ModelID: id, UpstreamID: id, Name: id, DisplayName: id,
			Object: "model", OwnedBy: "codebuddy", SupportsTools: true, Verified: id == "auto", Source: "config",
		})
	}
	return out
}

func (s *Service) ActivePoolSite() string {
	return config.NormalizeSite(s.Config().Site)
}

func (s *Service) ActiveProduct() string {
	return config.NormalizeProduct(s.Config().Product)
}

func (s *Service) SetPoolSite(site string) (map[string]any, error) {
	return s.persistPoolRouting(site, s.ActiveProduct(), "号池已切换到 "+config.NormalizeSite(site)+"，后续请求只使用该区域账号。")
}

func (s *Service) SetPoolProduct(product string) (map[string]any, error) {
	product = config.NormalizeProduct(product)
	label := "CodeBuddy CLI"
	if product == "workbuddy" {
		label = "WorkBuddy IDE"
	}
	return s.persistPoolRouting(s.ActivePoolSite(), product, "上游已切换到 "+label+"，国内/国际账号都会按该产品请求。")
}

func (s *Service) persistPoolRouting(site, product, note string) (map[string]any, error) {
	site = config.NormalizeSite(site)
	product = config.NormalizeProduct(product)
	baseURL := config.ProductPortalBaseURL(site, product)
	internet := ""
	if site == "domestic" {
		internet = "internal"
	}

	envPath := config.ResolveEnvFilePath()
	values := map[string]string{
		"CODEBUDDY_SITE":                 site,
		"CODEBUDDY_PRODUCT":              product,
		"CODEBUDDY_BASE_URL":             baseURL,
		"CODEBUDDY_INTERNET_ENVIRONMENT": internet,
	}
	if err := config.UpsertEnvFile(envPath, values); err != nil {
		return nil, fmt.Errorf("persist pool routing failed: %w", err)
	}

	s.updateConfig(func(c *config.Config) {
		c.Site = site
		c.Product = product
		c.BaseURL = baseURL
		c.InternetEnvironment = internet
	})
	s.invalidateModelsCache()
	_ = os.Setenv("CODEBUDDY_SITE", site)
	_ = os.Setenv("CODEBUDDY_PRODUCT", product)
	_ = os.Setenv("CODEBUDDY_BASE_URL", baseURL)
	_ = os.Setenv("CODEBUDDY_INTERNET_ENVIRONMENT", internet)

	s.Log.Debug("pool routing switched", "site", site, "product", product, "baseUrl", baseURL, "envFile", envPath)
	payload := s.Status()
	payload["ok"] = true
	payload["switched"] = true
	payload["envFile"] = envPath
	payload["note"] = note
	return payload, nil
}

func (s *Service) Status() map[string]any {
	cfg := s.Config()
	s.mu.Lock()
	stats := s.stats
	s.mu.Unlock()
	site := config.NormalizeSite(cfg.Site)
	product := config.NormalizeProduct(cfg.Product)
	baseURL := cfg.BaseURL
	internet := cfg.InternetEnvironment
	host := cfg.Host
	port := cfg.Port
	requireKey := cfg.RequireAPIKey
	accountsPath := cfg.AccountsPath
	chatPath := cfg.ChatCompletionsPath
	transport := cfg.Transport
	store, _ := s.Pool.Read()
	summary := accounts.SummarizeStoreForSite(store, s.Pool.Path(), site)
	return map[string]any{
		"ok":          true,
		"provider":    "codebuddy",
		"transport":   transport,
		"version":     version.Version,
		"build":       version.Info(),
		"uptimeMs":    time.Since(s.Started).Milliseconds(),
		"stats":       stats,
		"accounts":    summary,
		"poolSite":    site,
		"product":     product,
		"poolProduct": product,
		"config": map[string]any{
			"host":                host,
			"port":                port,
			"requireApiKey":       requireKey,
			"site":                site,
			"poolSite":            site,
			"product":             product,
			"poolProduct":         product,
			"baseUrl":             baseURL,
			"internetEnvironment": internet,
			"accountsPath":        accountsPath,
			"usagePath":           s.Config().UsagePath,
			"chatCompletionsPath": chatPath,
		},
	}
}

func (s *Service) BeginRequest(model string, promptChars int, stream bool) func(ok bool, outputChars int, upstreamBytes, eventCount, deltaCount int64, errMsg string, usage provider.Usage) {
	started := time.Now()
	s.mu.Lock()
	s.stats.TotalRequests++
	s.stats.ActiveRequests++
	s.stats.LastRequestAt = started.UnixMilli()
	s.stats.LastModel = model
	s.stats.LastPromptChars = int64(promptChars)
	s.stats.LastStream = stream
	s.stats.LastError = ""
	s.mu.Unlock()
	return func(ok bool, outputChars int, upstreamBytes, eventCount, deltaCount int64, errMsg string, usage provider.Usage) {
		duration := time.Since(started).Milliseconds()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.stats.ActiveRequests--
		s.stats.LastDurationMs = duration
		s.stats.TotalDurationMs += duration
		s.stats.LastOutputChars = int64(outputChars)
		s.stats.LastUpstreamBytes = upstreamBytes
		s.stats.LastEventCount = eventCount
		s.stats.LastDeltaCount = deltaCount
		if usage.PromptTokens > 0 || usage.CompletionTokens > 0 || usage.TotalTokens > 0 || usage.CachedTokens() > 0 {
			s.stats.LastPromptTokens = int64(usage.PromptTokens)
			s.stats.LastCompletionTokens = int64(usage.CompletionTokens)
			s.stats.LastCachedTokens = int64(usage.CachedTokens())
			s.stats.TotalPromptTokens += int64(usage.PromptTokens)
			s.stats.TotalCompletionTokens += int64(usage.CompletionTokens)
			total := usage.TotalTokens
			if total == 0 {
				total = usage.PromptTokens + usage.CompletionTokens
			}
			s.stats.TotalTokens += int64(total)
			s.stats.TotalCachedTokens += int64(usage.CachedTokens())
		}
		if ok {
			s.stats.SuccessRequests++
			s.stats.LastError = ""
		} else {
			s.stats.FailedRequests++
			s.stats.LastError = strutil.Truncate(errMsg, 400)
		}
	}
}

func emptyOAuthSession() OAuthSession {
	return OAuthSession{Status: "idle"}
}

// LiveOAuthSession 返回当前 OAuth 会话的值快照。
// 鉴权请优先用 OAuthLaunchAuthorized，避免共享指针竞态。
func (s *Service) LiveOAuthSession() OAuthSession {
	return s.CurrentOAuth()
}

func (s *Service) resetOAuthSessionLocked(site, label, publicOrigin string) {
	if s.oauth == nil {
		s.oauth = &OAuthSession{}
	}
	*s.oauth = emptyOAuthSession()
	s.oauth.ID = fmt.Sprintf("%d-%s", time.Now().UnixMilli(), strutil.RandomHex(4))
	s.oauth.Token = strutil.RandomHex(16)
	s.oauth.Status = "starting"
	s.oauth.Site = config.NormalizeSite(strutil.First(site, s.Config().Site, "global"))
	s.oauth.Label = strutil.First(label, "CodeBuddy OAuth")
	s.oauth.StartedAt = time.Now().UnixMilli()
	s.oauth.UpdatedAt = s.oauth.StartedAt
	s.oauth.LaunchURL = fmt.Sprintf("%s/direct-admin/codebuddy/oauth/launch?id=%s&token=%s", strings.TrimRight(publicOrigin, "/"), url.QueryEscape(s.oauth.ID), url.QueryEscape(s.oauth.Token))
	s.oauth.AccessURL = strings.TrimRight(publicOrigin, "/") + "/direct-admin/#codebuddy"
	s.oauth.CallbackURL = fmt.Sprintf("%s/direct-admin/codebuddy/oauth/callback?id=%s&token=%s", strings.TrimRight(publicOrigin, "/"), url.QueryEscape(s.oauth.ID), url.QueryEscape(s.oauth.Token))
}

func (s *Service) StartOAuth(ctx context.Context, site, label, publicOrigin string, reuse bool) (map[string]any, error) {
	s.mu.Lock()
	if s.oauth == nil {
		s.oauth = &OAuthSession{Status: "idle"}
	}
	if reuse && s.oauth.Status == "waiting" && s.oauth.AuthState != "" && time.Since(time.UnixMilli(s.oauth.StartedAt)) < s.Config().OAuthSessionTTL {
		payload := s.oauthPayloadLocked(publicOrigin)
		s.mu.Unlock()
		return payload, nil
	}
	// 持锁重置；launch/callback 通过 id+token 快照校验授权。
	s.resetOAuthSessionLocked(site, label, publicOrigin)
	sessionID := s.oauth.ID
	sessionSite := s.oauth.Site
	s.mu.Unlock()

	started, err := s.OAuth.Start(ctx, sessionSite)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.oauth == nil || s.oauth.ID != sessionID {
		return s.oauthPayloadLocked(publicOrigin), nil
	}
	if err != nil {
		s.oauth.Status = "failed"
		s.oauth.Error = err.Error()
		s.oauth.UpdatedAt = time.Now().UnixMilli()
		return s.oauthPayloadLocked(publicOrigin), nil
	}
	s.oauth.AuthState = started.AuthState
	s.oauth.URL = started.AuthURL
	s.oauth.Status = "waiting"
	s.oauth.Error = ""
	s.oauth.UpdatedAt = time.Now().UnixMilli()
	return s.oauthPayloadLocked(publicOrigin), nil
}

func (s *Service) PollOAuth(ctx context.Context, publicOrigin string) (map[string]any, error) {
	s.mu.Lock()
	if s.oauth == nil {
		s.oauth = &OAuthSession{Status: "idle"}
	}
	session := *s.oauth
	s.mu.Unlock()
	if session.Status != "waiting" || session.AuthState == "" {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.oauthPayloadLocked(publicOrigin), nil
	}
	polled, err := s.OAuth.Poll(ctx, session.Site, session.AuthState)
	if err != nil {
		s.mu.Lock()
		s.oauth.Status = "failed"
		s.oauth.Error = err.Error()
		s.oauth.UpdatedAt = time.Now().UnixMilli()
		payload := s.oauthPayloadLocked(publicOrigin)
		s.mu.Unlock()
		return payload, nil
	}
	if polled.Status == "pending" {
		s.mu.Lock()
		s.oauth.UpdatedAt = time.Now().UnixMilli()
		payload := s.oauthPayloadLocked(publicOrigin)
		s.mu.Unlock()
		return payload, nil
	}
	if polled.Status == "success" && polled.TokenData != nil {
		account := oauth.AccountFromTokenData(polled.TokenData, session.Site, session.Label)
		saved, store, err := s.Pool.Upsert(account)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.oauth.Status = "success"
		s.oauth.Error = ""
		s.oauth.ConfirmedAt = time.Now().UnixMilli()
		s.oauth.UpdatedAt = s.oauth.ConfirmedAt
		payload := s.oauthPayloadLocked(publicOrigin)
		s.mu.Unlock()
		payload["ok"] = true
		payload["account"] = accounts.SummarizeAccount(saved)
		payload["accounts"] = accounts.SummarizeStore(store, s.Pool.Path())
		return payload, nil
	}
	s.mu.Lock()
	s.oauth.Status = "failed"
	s.oauth.Error = strutil.First(polled.Message, "oauth poll failed")
	s.oauth.UpdatedAt = time.Now().UnixMilli()
	payload := s.oauthPayloadLocked(publicOrigin)
	s.mu.Unlock()
	return payload, nil
}

func (s *Service) OAuthSession(publicOrigin string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.oauthPayloadLocked(publicOrigin)
}

func (s *Service) OAuthLaunchAuthorized(id, token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.oauth == nil {
		return false
	}
	return id != "" && token != "" && id == s.oauth.ID && token == s.oauth.Token
}

func (s *Service) CurrentOAuth() OAuthSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.oauth == nil {
		return OAuthSession{Status: "idle"}
	}
	return *s.oauth
}

func (s *Service) oauthPayloadLocked(publicOrigin string) map[string]any {
	if s.oauth == nil {
		s.oauth = &OAuthSession{Status: "idle"}
	}
	session := *s.oauth
	if session.LaunchURL == "" && session.ID != "" && session.Token != "" && publicOrigin != "" {
		session.LaunchURL = fmt.Sprintf("%s/direct-admin/codebuddy/oauth/launch?id=%s&token=%s", strings.TrimRight(publicOrigin, "/"), url.QueryEscape(session.ID), url.QueryEscape(session.Token))
	}
	return map[string]any{
		"ok":      session.Status == "success" || session.Status == "waiting" || session.Status == "idle" || session.Status == "starting",
		"session": session,
		"login": map[string]any{
			"success":     session.Status == "waiting" || session.Status == "success",
			"message":     oauthMessage(session),
			"url":         session.URL,
			"externalUrl": session.URL,
			"launchUrl":   session.LaunchURL,
		},
	}
}

func oauthMessage(session OAuthSession) string {
	switch session.Status {
	case "waiting":
		return "请在打开的 CodeBuddy 页面完成登录，然后回到管理台点击「检查登录」或等待自动轮询。"
	case "success":
		return "CodeBuddy 登录成功，账号已写入账号池。"
	case "failed":
		return strutil.First(session.Error, "CodeBuddy OAuth 失败")
	case "starting":
		return "正在创建 CodeBuddy OAuth 会话…"
	default:
		return "尚未开始 CodeBuddy OAuth 登录。"
	}
}
