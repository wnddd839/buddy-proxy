package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
	"github.com/wnddd839/codebuddy-proxy/internal/concurrency"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/egress"
	"github.com/wnddd839/codebuddy-proxy/internal/modelroute"
	"github.com/wnddd839/codebuddy-proxy/internal/models"
	"github.com/wnddd839/codebuddy-proxy/internal/oauth"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/sessionpin"
	"github.com/wnddd839/codebuddy-proxy/internal/strutil"
	"github.com/wnddd839/codebuddy-proxy/internal/usagejournal"
	"github.com/wnddd839/codebuddy-proxy/internal/version"
	"github.com/wnddd839/codebuddy-proxy/internal/watch"
)

const (
	modelsCacheTTL = 60 * time.Second
	probeCacheTTL  = 20 * time.Second
	probeTimeout   = 4 * time.Second
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
	reProviderModel = regexp.MustCompile(`(?i)^codebuddy(?:(?:/|:)(.*))?$`)
	reAuthFailure   = regexp.MustCompile(`(?i)(?:\b401\b|\b403\b|unauthori[sz]ed|forbidden|token|credential|auth|login|not authenticated|未登录|登录|凭证)`)
	// "transport error" = Do 阶段（响应头之前）的连接层失败；"stream reset before
	// first content" = 已收到 200 头但上游在任何正文/工具增量之前掐断流
	// （provider.readSSE 标记，思考流不算增量）。两者客户端都尚未收到可消费的
	// 内容，重放不可见，换号重拨即可自愈。
	reRetryNextAccount = regexp.MustCompile(`(?i)\b429\b|\b500\b|\b502\b|\b503\b|\b504\b|rate.?limit|too many requests|site mismatch|invalid request|transport error|stream reset before first content`)
)

// maxResetReplays 慢掐重放预算：最多 2 次重放（共 3 发）。每次重放都会让思考
// 阶段从头再来（实测 15-20 分钟墙钟），并对冷缓存重烧全量 prompt（实测一条链
// 把缓存命中率从 99% 打到 6.5%）。遍历所有账号能把单个请求拼成数小时；超预算
// re429 匹配配额/限流类状态码与上游错误码，供跨区判定使用。
// 与 reRetryNextAccount 分开：那个决定「换不换号」，这个决定「换不换区」。
var re429 = regexp.MustCompile(`(?i)\b429\b|\b6004\b|rate.?limit|too many requests|quota.?exhausted|使用量已超出频率限制`)

const maxResetReplays = 2

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
	Pool *accounts.Pool
	// ModelRoutes 是跨区模型路由表（proxy-model-routes.json）。
	// 命中时改写请求的模型名与站点；nil 表示未加载（跳过路由）。
	ModelRoutes *modelroute.File
	Provider    *provider.Client
	OAuth       *oauth.Client
	// oauthGlobal 是国际站专用的 OAuth 客户端（走 OAuthProxy 出口）。
	// 与 OAuth 分开是因为 oauth.Client 会记住上一次 state 做去重，
	// 两个站点的 state 空间不能混；nil 表示未配置出口，回退到 OAuth。
	oauthGlobal *oauth.Client
	Models      *models.Lister
	Log         *slog.Logger
	Started     time.Time

	runtimeCfg atomic.Pointer[config.Config]

	mu    sync.Mutex
	stats Stats
	oauth *OAuthSession

	modelsCacheMu sync.Mutex
	modelsCache   map[string]modelsCacheEntry
	modelsFlight  modelsFlight
	Pins          *sessionpin.Table
	// Conversations 让钉住的会话在账号不变时复用上游 X-Conversation-ID。
	Conversations *sessionpin.ConversationTable
	Journal       *usagejournal.Journal
	// Watch 是进程内波动监测器，nil 表示未启用（见 internal/watch）。
	Watch *watch.Monitor
	// Concurrency 是进程内按账号并发观测（无落盘）。
	Concurrency *concurrency.Tracker

	probeMu    sync.Mutex
	probeCache map[string]provider.Reachability
}

type modelsCacheEntry struct {
	result    models.ListResult
	expiresAt time.Time
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
		Pool:        accounts.NewPool(cfg.AccountsPath),
		Concurrency: concurrency.New(),
		Provider:    p,
		OAuth:       oauth.NewClient(p.HTTP),
		Models:      models.NewLister(),
		Log:         logger,
		Started:     time.Now(),
		oauth:       &OAuthSession{Status: "idle"},
		Pins:        sessionpin.New(0),
		// 与账号钉相同 TTL（45 分钟空闲）；账号切换后 ID 会轮换。
		Conversations: sessionpin.NewConversationTable(0),
		Journal:       journal,
	}
	if cfg.Watch.Enabled {
		svc.Watch = watch.New(cfg.Watch, logger, nil)
	}
	// 国际站 OAuth 必须走海外出口：www.codebuddy.ai 从大陆直连会 TLS 握手超时。
	// p.For 按 proxy URL 缓存 http.Client，重复构造不会新建连接池。
	// OAuth 走独立出口（可选）。代理串无效时必须退回默认客户端，
	// 不能造出一个连不上的 fail-closed 客户端 —— 那样整个 OAuth 流程会挂死。
	if proxy := strings.TrimSpace(cfg.OAuthProxy); proxy != "" {
		if _, err := egress.Parse(proxy); err != nil {
			logger.Error("invalid CODEBUDDY_PROXY_OAUTH_PROXY; global OAuth uses the default client", "error", err.Error())
		} else {
			svc.oauthGlobal = oauth.NewClient(p.For(proxy))
		}
	}
	// 并发观测始终启用：只做计数，成本可忽略。
	svc.modelsCache = map[string]modelsCacheEntry{}
	svc.probeCache = map[string]provider.Reachability{}
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

// WatchStatus 返回波动监测状态；未启用时 Enabled=false。
func (s *Service) WatchStatus() watch.Status {
	if s == nil || s.Watch == nil {
		return watch.Status{}
	}
	return s.Watch.Snapshot()
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

// SetAPIKeys 热更新 CODEBUDDY_PROXY_API_KEYS 绑定表，不改主 Key。
// BehaviorSwitches 是可在管理台热切换的行为开关。
// 用结构体而不是 4 个独立 setter：这些开关总是一起读、一起写。
type BehaviorSwitches struct {
	CheckinAllowDirectFallback bool
	QuotaFloorCredits          float64
	// 签到失败重试：间隔 0 = 不重试。
	CheckinRetryInterval time.Duration
	CheckinRetryMax      int
}

// SetBehaviorSwitches 更新运行中的行为开关（.env 由调用方负责持久化）。
func (s *Service) SetBehaviorSwitches(next BehaviorSwitches) config.Config {
	cfg := s.updateConfig(func(c *config.Config) {
		c.CheckinAllowDirectFallback = next.CheckinAllowDirectFallback
		c.QuotaFloorCredits = next.QuotaFloorCredits
		c.CheckinRetryInterval = next.CheckinRetryInterval
		c.CheckinRetryMax = next.CheckinRetryMax
	})
	// 积分阈值直接作用于选号，热更新立即生效。
	if s.Pool != nil {
		s.Pool.SetQuotaFloor(next.QuotaFloorCredits)
	}
	return cfg
}

func (s *Service) SetAPIKeys(keys []config.APIKeyBinding) config.Config {
	copied := append([]config.APIKeyBinding(nil), keys...)
	return s.updateConfig(func(c *config.Config) {
		c.APIKeys = copied
	})
}

type ProviderModel struct {
	Provider    string
	Model       string
	PublicModel string
	Site        string
}

func ResolveProviderModel(model string) ProviderModel {
	cleaned := strings.TrimSpace(model)
	if match := reProviderModel.FindStringSubmatch(cleaned); match != nil {
		requested := strings.TrimSpace(match[1])
		if requested == "" || requested == "default" {
			requested = "auto"
		}
		cleaned = requested
	}
	if cleaned == "" {
		return ProviderModel{Provider: "codebuddy", Model: "auto", PublicModel: "auto"}
	}
	if site, rest, ok := config.SplitSitePrefix(cleaned); ok {
		publicID := models.PublicModelID(rest)
		return ProviderModel{
			Provider:    "codebuddy",
			Model:       publicID,
			PublicModel: config.SiteModelPrefix(site) + ":" + publicID,
			Site:        site,
		}
	}
	publicID := models.PublicModelID(cleaned)
	return ProviderModel{Provider: "codebuddy", Model: publicID, PublicModel: publicID}
}

type CompleteOptions struct {
	AccountID    string
	FixedAccount bool // API Key 的硬约束，不允许清钉或换号降级
	SessionKey   string
	SessionLabel string
	Site         string // 本请求号池区域；空则回落进程默认 SITE
	// SiteExplicit 表示站点是客户端显式指定的（cn:/global: 模型前缀，或 X-Site 请求头）。
	// 显式指定站点时绝不套用路由表：客户端要哪个区就给它哪个区，
	// 不能被「某区→某区」的规则悄悄改到另一个区去。
	SiteExplicit        bool
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
	// DeliverToolDeltas 透传给 provider：调用方真的会转发 tool_call_delta 时为 true。
	DeliverToolDeltas bool
	RefreshRetry      bool
	RetryDepth        int // internal recursion counter; callers must not set
	ResetReplays      int // 建连与首内容前断流共同消耗的重放预算。
	// EmptyTurnReplays 空 completed 回合已重放次数（独立于 RetryDepth，
	// 避免 429/流重置/OAuth 刷新等无关失败提前耗尽）。
	EmptyTurnReplays int
	// RoutedFrom 记录路由改写前的原始模型名（未命中路由时为空）。
	RoutedFrom     string
	routeFallbacks []modelroute.Rule // Remaining rules from the original source/model snapshot.
	// deferredRoute 是 mode=onExhausted 的整条路由链：先按源站点原模型跑，
	// 只有判定「源站点用尽」时才推进。用一条少一条，天然不会来回弹跳。
	deferredRoute []modelroute.Rule
}

// ImageOptions is one pooled image generation or edit.
type ImageOptions struct {
	Site         string
	Model        string
	Prompt       string
	ImageURLs    []string
	ImageData    []string
	AccountID    string
	FixedAccount bool
	ExcludeIDs   []string
	RefreshRetry bool
	RetryDepth   int
}

// ImageResult is a pooled upstream image response plus the account that served it.
type ImageResult struct {
	provider.ImageResult
	Account   accounts.Summary `json:"account"`
	AccountID string           `json:"accountId"`
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
		// 账号级出口：同一账号始终走同一个代理，不随请求轮换。
		Proxy: account.Proxy,
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
		DeliverToolDeltas:  opts.DeliverToolDeltas,
	}
}

type CompleteResult struct {
	provider.Result
	Account   accounts.Summary `json:"account"`
	AccountID string           `json:"accountId"`
}

func (s *Service) requestSite(opts CompleteOptions) string {
	if site := config.OptionalSite(opts.Site); site != "" {
		return site
	}
	return s.ActivePoolSite()
}

// SessionPinKey 把会话钉号按区域隔开，避免同一 SessionKey 并行打 cn: 与 global: 时抢 pin。
func SessionPinKey(session, site string) string {
	session = strings.TrimSpace(session)
	if session == "" {
		return ""
	}
	return session + "\x1e" + config.NormalizeSite(site)
}

func (s *Service) CompleteFromPool(ctx context.Context, opts CompleteOptions) (CompleteResult, error) {
	opts.Model = strutil.First(opts.Model, "auto")
	// 模型路由：用户在管理台配置「源站点 + 源模型名 → 目标站点 + 目标模型名」。
	//
	// 只在逻辑请求入口匹配一次；递归重试继续使用已选定的目标。
	// 同源区域和模型的多条规则构成有序备用链，仅在目标无可用账号时推进，
	// 不把一般上游错误当作跨区降级信号。
	//
	// 两条硬规则：
	//  1. 模型名带 cn:/global: 前缀（客户端硬钉站点）时完全不套用路由；
	//  2. 规则必须写明源站点，只有请求本来就落在该站点时才改写。
	if s.ModelRoutes != nil && !opts.FixedAccount && !opts.SiteExplicit && opts.RetryDepth == 0 && opts.RoutedFrom == "" {
		from := config.NormalizeSite(s.requestSite(opts))
		if chain := s.ModelRoutes.Chain(from, opts.Model); len(chain) > 0 {
			if chain[0].OnExhausted() {
				// 「源站点用尽才跳」：先按原站点原模型选号，选不到或中途额度耗尽
				// 才把链推进到目标站点/模型。链首也留着，不预先改写。
				opts.deferredRoute = chain
				s.Log.Info("model route deferred until source exhausted",
					"fromSite", from, "match", opts.Model, "hops", len(chain))
			} else {
				rule := chain[0]
				opts.routeFallbacks = chain[1:]
				orig := opts.Model
				if t := strings.TrimSpace(rule.Target); t != "" {
					opts.Model = t
				}
				if site := strings.TrimSpace(rule.Site); site != "" {
					opts.Site = config.NormalizeSite(site)
				}
				opts.RoutedFrom = orig
				s.Log.Info("model route applied",
					"fromSite", from, "match", orig, "toSite", opts.Site, "target", opts.Model)
			}
		}
	}
	if opts.FixedAccount && opts.AccountID == "" {
		return CompleteResult{}, errors.New("固定账号请求缺少账号约束")
	}
	if opts.RetryDepth >= defaultMaxAccountRetries {
		return CompleteResult{}, fmt.Errorf("%w（max=%d）", errRetryDepthExceeded, defaultMaxAccountRetries)
	}
	if opts.SessionKey == "" {
		opts.SessionKey = sessionpin.Key(nil, "", opts.Messages)
	}
	site := s.requestSite(opts)
	pinKey := SessionPinKey(opts.SessionKey, site)
	if opts.AccountID == "" && pinKey != "" {
		if id, ok := s.Pins.Lookup(pinKey); ok && !slices.Contains(opts.ExcludeIDs, id) {
			opts.AccountID = id
		} else if ok {
			s.Pins.Forget(pinKey)
		}
	}
	if opts.AccountID == "" && opts.RetryDepth == 0 && s.needsQuotaRefresh(opts.ExcludeIDs, site) {
		s.refreshCandidateQuotas(ctx, opts.ExcludeIDs, true, site)
	}
	selection, err := s.Pool.Select(accounts.SelectOptions{
		AccountID:     opts.AccountID,
		Site:          site,
		ExcludeIDs:    opts.ExcludeIDs,
		PreferQuota:   opts.AccountID == "",
		Model:         opts.Model,
		CheckCooldown: !opts.FixedAccount,
	})
	if err != nil {
		// 软钉号不可用时先清钉；冷却账号仍保留普通池的全冷却兜底资格。
		if !opts.FixedAccount && opts.AccountID != "" && isPinnedAccountUnusable(err) {
			staleID := opts.AccountID
			if pinKey != "" {
				s.Pins.Forget(pinKey)
			}
			s.Log.Warn("clearing pinned account after it became unusable",
				"accountId", staleID,
				"site", site,
				"pin_cleared_reason", pinnedAccountUnusableReason(err),
				"error", err.Error(),
			)
			if !errors.Is(err, accounts.ErrAccountCoolingDown) {
				opts.ExcludeIDs = append(append([]string{}, opts.ExcludeIDs...), staleID)
			}
			opts.AccountID = ""
			opts.RetryDepth++
			return s.CompleteFromPool(ctx, opts)
		}
		// 两条链（always 的 routeFallbacks 与 onExhausted 的 deferredRoute）
		// 必须都能推进，否则 onExhausted 规则配成多跳时第二跳永远不会被消费。
		if routeExhausted(err) {
			if s.advanceRouteFallback(&opts) || s.advanceDeferredRoute(&opts) {
				return s.CompleteFromPool(ctx, opts)
			}
		}
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
	// 同会话复用上游会话 ID；仅在有账号钉（pinKey 非空）时注入，与钉号生命周期一致。
	if pinKey != "" {
		chatOpts.ConversationID = s.Conversations.ID(pinKey+"\x1e"+chatOpts.Product, account.ID)
	}
	// 并发观测：在这里打点，才能把「限流触发瞬间的在途数」和真实请求绑定。
	endFlight := s.Concurrency.Begin(account.ID, account.Label)
	result, err := s.Provider.Complete(ctx, chatOpts)
	if err != nil {
		// 必须在 endFlight 之前记录：此时本次请求仍在途，才能反映「限流触发瞬间的真实并发」。
		s.recordRateLimit(account, err)
	}
	endFlight(err == nil)
	if err != nil {
		// 客户端主动断开 SSE/HTTP，不算账号或上游故障。
		if errors.Is(err, context.Canceled) || strings.Contains(strings.ToLower(err.Error()), "context canceled") {
			s.Log.Info("codebuddy request canceled by client", "accountId", account.ID, "model", opts.Model)
			return CompleteResult{}, err
		}
		// 已经发给客户端的正文或工具不能撤回，任何错误都不得再换号或刷新后重放。
		if result.Turn.Delivered {
			_ = s.Pool.MarkResult(selection, false, err.Error(), 0)
			return CompleteResult{Result: result, Account: accounts.SummarizeAccount(account), AccountID: account.ID}, err
		}
		selection.ModelRateLimited = billing.IsModelRateLimitError(err)
		// 传输层失败（建连 / 首内容前断流）⇒ 用**同一账号**重连一次。
		//
		// 背景：机场出口（尤其日本节点）单次连接会突然 EOF，实测同一节点
		// 连打 6 次可能挂 1-2 次。重连一次通常就过。
		// 这里只重连、不换号——站内换号重试已经移除，换号只会烧额度并让
		// 会话亲和失效。只对「首内容前」的失败生效（Delivered 已提前返回）。
		if isTransportReplayError(err.Error()) && opts.ResetReplays < maxResetReplays {
			opts.ResetReplays++
			opts.RetryDepth++
			opts.AccountID = account.ID
			s.Log.Warn("retrying same account after transport error",
				"accountId", account.ID, "site", site, "attempt", opts.ResetReplays, "error", err.Error())
			return s.CompleteFromPool(ctx, opts)
		}
		// 额度类失败（429 / 6004 / 额度耗尽）⇒ 换下一个账号再试。
		//
		// 只对这一类换号：账号额度用尽是账号状态，换一个还有额度的号是正确做法；
		// 而且只有试遍所有候选，才能区分「号池空了」与「这个模型在限流窗口内」，
		// 后者要返回带 Retry-After 的冷却提示而不是继续打上游。
		// 其他错误（5xx / 111xx / 传输层）一律不换号，直接把错误返回客户端。
		if !opts.FixedAccount && isQuotaExhausted(err) {
			next := append(append([]string{}, opts.ExcludeIDs...), account.ID)
			if id, ok := s.nextSelectableAccount(site, opts.Model, next); ok {
				_ = s.Pool.MarkResult(selection, false, err.Error(), s.resolveFailureCooldown(ctx, account, err))
				if pinKey != "" {
					s.Pins.Forget(pinKey)
				}
				s.Log.Warn("retrying with next account after quota failure",
					"from", account.ID, "to", id, "site", site, "model", opts.Model, "error", err.Error())
				opts.ExcludeIDs = next
				opts.AccountID = id
				opts.RetryDepth++
				return s.CompleteFromPool(ctx, opts)
			}
			// 同区已经没有可换的号了：本次失败才算「源站点用尽」，
			// 这时才允许 mode=onExhausted 的路由改写到另一个站点。
			// （请求一进来就换区会把本来能用的源站点号池绕过去。）
			if s.advanceDeferredRoute(&opts) {
				_ = s.Pool.MarkResult(selection, false, err.Error(), s.resolveFailureCooldown(ctx, account, err))
				if pinKey != "" {
					s.Pins.Forget(pinKey)
				}
				return s.CompleteFromPool(ctx, opts)
			}
		}
		// 空 completed 回合（上游发了收尾帧但没有任何内容）：同一账号重放一次。
		// 这是上游的收尾帧不可信，不是账号问题，所以不罚号。
		// 独立预算（EmptyTurnReplays），避免 429/流重置等无关失败提前把它耗尽。
		if strings.Contains(strings.ToLower(err.Error()), "empty completed turn") &&
			opts.EmptyTurnReplays == 0 {
			opts.EmptyTurnReplays++
			opts.RetryDepth++
			opts.AccountID = account.ID
			s.Log.Warn("retrying same account after empty completed turn",
				"accountId", account.ID, "site", site, "error", err.Error())
			return s.CompleteFromPool(ctx, opts)
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
	if pinKey != "" {
		s.Pins.Remember(pinKey, account.ID)
	}
	return CompleteResult{
		Result:    result,
		Account:   accounts.SummarizeAccount(account),
		AccountID: account.ID,
	}, nil
}

// recordRateLimit 只把确认的限流写入观测器，且必须在 endFlight 之前调用。
func (s *Service) recordRateLimit(account accounts.Account, err error) {
	if s == nil || s.Concurrency == nil || err == nil {
		return
	}
	msg := strings.ToLower(err.Error())
	if !billing.IsModelRateLimitError(err) && !strings.Contains(msg, "429") &&
		!strings.Contains(msg, "too many requests") && !strings.Contains(msg, "rate limit") {
		return
	}
	s.Concurrency.RecordRateLimit(account.ID, err.Error())
}

// ConcurrencyStatus 返回完整快照，供管理台按需读取。
func (s *Service) ConcurrencyStatus() concurrency.Snapshot {
	if s == nil {
		return concurrency.Snapshot{}
	}
	return s.Concurrency.Snapshot()
}

// MergeRefreshedToken 把刷新得到的凭证合并回已有账号。
// 只更新凭证相关字段，保留运维字段（启用状态、出口、配额、冷却、租户信息等）——
// AccountFromTokenData 只认识 token，直接 ReplaceAccount 会把这些字段清掉。
func MergeRefreshedToken(existing accounts.Account, token *oauth.TokenData) accounts.Account {
	updated := oauth.AccountFromTokenData(token, strutil.First(existing.Site), existing.Label)
	updated.ID = existing.ID
	updated.Enabled = existing.Enabled
	updated.Source = strutil.First(existing.Source, updated.Source)
	updated.Site = strutil.First(existing.Site, updated.Site)
	updated.BaseURL = existing.BaseURL
	updated.InternetEnvironment = existing.InternetEnvironment
	updated.APIEndpoint = existing.APIEndpoint
	updated.ChatCompletionsPath = existing.ChatCompletionsPath
	updated.Transport = existing.Transport
	updated.EnterpriseID = existing.EnterpriseID
	updated.TenantID = existing.TenantID
	updated.DepartmentFullName = existing.DepartmentFullName
	updated.CreatedAt = existing.CreatedAt
	updated.SuccessRequests = existing.SuccessRequests
	updated.FailedRequests = existing.FailedRequests
	updated.LastUsedAt = existing.LastUsedAt
	updated.LastSelectedAt = existing.LastSelectedAt
	updated.CooldownUntil = existing.CooldownUntil
	updated.CooldownSource = existing.CooldownSource
	updated.ModelCooldowns = existing.ModelCooldowns
	updated.QuotaRemaining = existing.QuotaRemaining
	updated.QuotaUnlimited = existing.QuotaUnlimited
	updated.QuotaResetAt = existing.QuotaResetAt
	updated.QuotaCheckedAt = existing.QuotaCheckedAt
	// 出口是运维决策，刷新令牌不该把号扔回本机 IP。
	updated.Proxy = existing.Proxy
	return updated
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
		// 国际站优先使用 OAuth 专用出口；未配置时沿用账号固定出口。
		HTTPClient: s.RefreshHTTPFor(account),
	})
	if err != nil {
		if force || (account.TokenExpiresAt > 0 && account.TokenExpiresAt <= time.Now().UnixMilli()) {
			return selection, fmt.Errorf("CodeBuddy OAuth refresh failed; please re-authenticate from /direct-admin/#codebuddy. %w", err)
		}
		s.Log.Warn("codebuddy oauth refresh skipped after failure", "accountId", account.ID, "error", err.Error())
		return selection, nil
	}
	saved, _, err := s.SaveRefreshedToken(account, token)
	if err != nil {
		return selection, err
	}
	selection.Account = saved
	s.Log.Info("codebuddy oauth credential refreshed", "accountId", saved.ID, "site", saved.Site)
	return selection, nil
}

// SaveRefreshedToken preserves concurrent operational edits and never overwrites newer credentials.
func (s *Service) SaveRefreshedToken(previous accounts.Account, token *oauth.TokenData) (accounts.Account, accounts.Store, error) {
	return s.Pool.UpdateAccount(previous.ID, func(current accounts.Account) accounts.Account {
		if current.BearerToken != previous.BearerToken || current.RefreshToken != previous.RefreshToken {
			return current
		}
		return MergeRefreshedToken(current, token)
	})
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

// isTransportReplayError 判断是否属于"传输层可重放"失败：建连失败
// （transport error）与流在首内容前被重置（stream reset before first content）。
// 两类共同消耗 maxResetReplays 预算——否则多账号场景会按账号数放大请求数。
func isTransportReplayError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "transport error") ||
		strings.Contains(lower, "stream reset before first content") ||
		// 无终止帧就结流（上游 FIN 半截）：首内容前等同建连失败，可安全换号重放。
		strings.Contains(lower, "without terminal event before first content") ||
		// 上游停滞（看门狗掐断）：首内容前同样可安全换号。
		strings.Contains(lower, "stream stalled before first content")
}

func isSiteMismatchError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "site mismatch")
}

func pinnedAccountUnusableReason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, accounts.ErrQuotaBelowFloor):
		return "quota_below_floor"
	case errors.Is(err, accounts.ErrAccountDisabled):
		return "disabled"
	case errors.Is(err, accounts.ErrAccountNotFound):
		return "not_found"
	case errors.Is(err, accounts.ErrNoCredentials):
		return "no_credentials"
	case errors.Is(err, accounts.ErrAccountCoolingDown):
		return "cooldown"
	case isSiteMismatchError(err):
		return "site_mismatch"
	default:
		return ""
	}
}

func isPinnedAccountUnusable(err error) bool {
	return pinnedAccountUnusableReason(err) != ""
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

// hasAlternativeAccount 判断该站点除当前账号外还有没有能用的候选。
//
// 只读实现（不走 Pool.Select）：Select 会写 LastSelectedAt，拿它做探测会污染
// 选号统计。判据与 Select 保持一致（accounts.UsableForModel）。
func (s *Service) hasAlternativeAccount(site, currentID string, exclude []string) bool {
	if s == nil || s.Pool == nil {
		return false
	}
	store, err := s.Pool.Read()
	if err != nil {
		return false
	}
	skip := make(map[string]struct{}, len(exclude)+1)
	skip[currentID] = struct{}{}
	for _, id := range exclude {
		skip[id] = struct{}{}
	}
	now := time.Now().UnixMilli()
	floor := s.Pool.QuotaFloor()
	want := config.NormalizeSite(site)
	for _, item := range store.Accounts {
		if _, ok := skip[item.ID]; ok {
			continue
		}
		if config.NormalizeSite(item.Site) != want {
			continue
		}
		if !accounts.UsableForModel(item, now, "", floor) {
			continue
		}
		return true
	}
	return false
}

func (s *Service) resolveFailureCooldown(ctx context.Context, account accounts.Account, err error) time.Duration {
	base := failureCooldown(err)
	if d, ok := billing.ModelRateLimitCooldown(err, time.Now()); ok {
		s.logFailureCooldown(account.ID, d, "error_text")
		return d
	}
	msg := ""
	if err != nil {
		msg = strings.ToLower(err.Error())
	}
	if strings.Contains(msg, "6004") {
		if base <= 0 {
			base = 2 * time.Minute
		}
		s.logFailureCooldown(account.ID, base, "fixed_2m")
		return base
	}
	if s == nil || s.Pool == nil || s.Provider == nil || !billing.IsQuotaExhaustedError(err) {
		if base == 2*time.Minute {
			s.logFailureCooldown(account.ID, base, "fixed_2m")
		}
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

func (s *Service) logFailureCooldown(accountID string, d time.Duration, source string) {
	if s == nil || s.Log == nil {
		return
	}
	s.Log.Info("account failure cooldown",
		"accountId", accountID,
		"cooldown", d.String(),
		"cooldown_source", source,
	)
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

func (s *Service) listSelectableAccounts(exclude []string, site string) []accounts.Account {
	if s == nil || s.Pool == nil {
		return nil
	}
	store, err := s.Pool.Read()
	if err != nil {
		return nil
	}
	site = config.NormalizeSite(strutil.First(site, s.ActivePoolSite()))
	now := time.Now().UnixMilli()
	floor := s.Pool.QuotaFloor()
	skip := map[string]struct{}{}
	for _, id := range exclude {
		if id != "" {
			skip[id] = struct{}{}
		}
	}
	out := make([]accounts.Account, 0, len(store.Accounts))
	for _, account := range store.Accounts {
		if site != "" && config.NormalizeSite(account.Site) != site {
			continue
		}
		if _, ok := skip[account.ID]; ok {
			continue
		}
		// model 传空：这里回答的是「这个号整体上还能不能干活」，
		// 模型级冷却由调用方按具体模型再判一次。
		if !accounts.UsableForModel(account, now, "", floor) {
			continue
		}
		out = append(out, account)
	}
	return out
}

// maxQuotaRefreshCooled 限制每次补探额外带上几个额度冷却号。
// 长期真耗尽的号不该让每个新会话都多等一次上游探测。
const maxQuotaRefreshCooled = 4

// listQuotaRefreshCandidates 返回「值得打一次额度接口」的号。
//
// includeCooled 时额外带上 quota 来源的冷却号：它们的余额可能在上游 resetAt
// 之前就被补回来，但 Select 永远选不到冷却号 ⇒ 只有额度快照能让冷却自动解除。
// 换号路径（staleOnly=false）不带，避免每次换号都多打几次上游。
func (s *Service) listQuotaRefreshCandidates(exclude []string, site string, includeCooled bool) []accounts.Account {
	out := s.listSelectableAccounts(exclude, site)
	if !includeCooled {
		return out
	}
	store, err := s.Pool.Read()
	if err != nil {
		return out
	}
	site = config.NormalizeSite(strutil.First(site, s.ActivePoolSite()))
	now := time.Now().UnixMilli()
	skip := make(map[string]struct{}, len(out)+len(exclude))
	for _, id := range exclude {
		skip[id] = struct{}{}
	}
	for _, account := range out {
		skip[account.ID] = struct{}{}
	}
	extra := 0
	for _, account := range store.Accounts {
		if extra >= maxQuotaRefreshCooled {
			break
		}
		if !account.Enabled || !accounts.HasCredentials(account) {
			continue
		}
		if site != "" && config.NormalizeSite(account.Site) != site {
			continue
		}
		if _, ok := skip[account.ID]; ok {
			continue
		}
		if account.CooldownSource != accounts.CooldownSourceQuota || !accounts.InCooldown(account, now) {
			continue
		}
		out = append(out, account)
		skip[account.ID] = struct{}{}
		extra++
	}
	return out
}

// needsQuotaRefresh 任一候选缺快照或超过 TTL，新会话就要补探，避免只查过部分号时 PreferQuota 看不见更厚的号。
func (s *Service) needsQuotaRefresh(exclude []string, site string) bool {
	now := time.Now().UnixMilli()
	for _, account := range s.listQuotaRefreshCandidates(exclude, site, true) {
		if !quotaSnapshotFresh(account, now) {
			return true
		}
	}
	return false
}

// refreshCandidateQuotas 并行刷新候选额度。staleOnly 时只打缺失/过期的号（新会话）；换号时刷全部活号。
func (s *Service) refreshCandidateQuotas(ctx context.Context, exclude []string, staleOnly bool, site string) {
	if s == nil || s.Pool == nil || s.Provider == nil {
		return
	}
	now := time.Now().UnixMilli()
	candidates := s.listQuotaRefreshCandidates(exclude, site, staleOnly)
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

const adminUsageRefreshTimeout = 15 * time.Second

// PoolUsageItem 是管理台「刷新状态」拉回来的单账号上游额度。
type PoolUsageItem struct {
	OK               bool               `json:"ok"`
	AccountID        string             `json:"accountId"`
	Site             string             `json:"site,omitempty"`
	Credits          billing.Credits    `json:"credits,omitzero"`
	Notify           *billing.Notify    `json:"notify,omitzero"`
	OfficialUsageURL string             `json:"officialUsageUrl,omitempty"`
	Note             string             `json:"note,omitempty"`
	Quota            billing.QuotaState `json:"quota,omitzero"`
	Error            string             `json:"error,omitzero"`
}

// RefreshPoolUsage 对已启用且有凭据的账号并行打上游套餐接口，写入 QuotaState。
// site 为空刷新全部站点。冷却中 / 配额耗尽的号也会打，方便页面看到是否已恢复。
// 15 秒自动轮询不要走这里。
func (s *Service) RefreshPoolUsage(ctx context.Context, site string) []PoolUsageItem {
	if s == nil || s.Pool == nil || s.Provider == nil {
		return nil
	}
	accounts := s.listPoolAccountsForUsage(site)
	if len(accounts) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, adminUsageRefreshTimeout)
	defer cancel()
	started := time.Now()
	items := make([]PoolUsageItem, len(accounts))
	var wg sync.WaitGroup
	var refreshed atomic.Int32
	for i, account := range accounts {
		wg.Go(func() {
			usage, err := billing.FetchAccountUsage(ctx, s.Provider, account, s.Config())
			if err != nil {
				items[i] = PoolUsageItem{AccountID: account.ID, Site: account.Site, Error: err.Error()}
				return
			}
			state := billing.QuotaStateFromUsage(usage, time.Now())
			// 「刷新状态」= 以刚取到的余额为准重置**账号级**冷却，再重新落 quota 状态。
			// 顺序不能反：先落 quota 会把旧冷却当成已有的更晚截止时刻保留下来。
			// 模型级 ModelCooldowns 不在此处处理 —— 它是真实 429 的证据，只按到期
			// 时间或真实请求结果解除。
			if _, clearErr := s.Pool.ClearCooldowns(account.ID); clearErr != nil {
				s.Log.Debug("admin cooldown clear failed", "accountId", account.ID, "error", clearErr.Error())
			}
			if _, applyErr := s.Pool.ApplyQuotaState(account.ID, state.Remaining, state.Unlimited, state.ResetAt, state.CheckedAt); applyErr != nil {
				s.Log.Debug("admin quota apply failed", "accountId", account.ID, "error", applyErr.Error())
			}
			items[i] = PoolUsageItem{
				OK:               usage.OK,
				AccountID:        account.ID,
				Site:             usage.Site,
				Credits:          usage.Credits,
				Notify:           usage.Notify,
				OfficialUsageURL: usage.OfficialUsageURL,
				Note:             usage.Note,
				Quota:            state,
			}
			refreshed.Add(1)
		})
	}
	wg.Wait()
	s.Log.Info("refreshed pool usage from upstream",
		"accounts", len(accounts),
		"updated", refreshed.Load(),
		"elapsed", time.Since(started).String(),
	)
	return items
}

func (s *Service) listPoolAccountsForUsage(site string) []accounts.Account {
	if s == nil || s.Pool == nil {
		return nil
	}
	store, err := s.Pool.Read()
	if err != nil {
		return nil
	}
	// site 为空 = 全部站点：管理台「刷新」要看到每个号的真实余额，
	// 只刷当前号池会把另一区的号留在过期快照上。
	site = config.OptionalSite(site)
	out := make([]accounts.Account, 0, len(store.Accounts))
	for _, account := range store.Accounts {
		if !account.Enabled || !accounts.HasCredentials(account) {
			continue
		}
		if site != "" && config.NormalizeSite(account.Site) != site {
			continue
		}
		out = append(out, account)
	}
	return out
}

func (s *Service) ListModels(ctx context.Context, fresh bool) (models.ListResult, error) {
	return s.ListModelsForSite(ctx, "", fresh)
}

// ListModelsForSite 返回单个区域的无前缀目录。site 为空时用进程默认 SITE。
func (s *Service) ListModelsForSite(ctx context.Context, site string, fresh bool) (models.ListResult, error) {
	if strings.TrimSpace(site) == "" {
		site = s.ActivePoolSite()
	}
	return s.listModelsForSite(ctx, config.NormalizeSite(site), fresh)
}

func (s *Service) credentialedSites() []string {
	store, err := s.Pool.Read()
	if err != nil {
		return []string{s.ActivePoolSite()}
	}
	seen := map[string]bool{}
	var sites []string
	for _, account := range store.Accounts {
		if !account.Enabled || !accounts.HasCredentials(account) {
			continue
		}
		site := config.NormalizeSite(account.Site)
		if seen[site] {
			continue
		}
		seen[site] = true
		sites = append(sites, site)
	}
	if len(sites) == 0 {
		return []string{s.ActivePoolSite()}
	}
	def := s.ActivePoolSite()
	slices.Sort(sites)
	out := make([]string, 0, len(sites))
	if slices.Contains(sites, def) {
		out = append(out, def)
	}
	for _, site := range sites {
		if site != def {
			out = append(out, site)
		}
	}
	return out
}

func (s *Service) listModelsForSite(ctx context.Context, activeSite string, fresh bool) (models.ListResult, error) {
	store, err := s.Pool.Read()
	if err != nil {
		return models.ListResult{}, err
	}
	activeSite = config.NormalizeSite(activeSite)
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
	cacheKey := strings.Join([]string{activeSite, product, account.ID, site, baseURL, internet, account.Proxy}, "|")
	if !fresh {
		if cached, ok := s.cachedModels(cacheKey); ok {
			return cached, nil
		}
	}

	listed, err, _ := s.modelsFlight.Do(cacheKey, func() (models.ListResult, error) {
		if !fresh {
			if cached, ok := s.cachedModels(cacheKey); ok {
				return cached, nil
			}
		}
		out := s.Models.List(ctx, s.Provider, models.ListOptions{
			Proxy:               account.Proxy,
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
			Fresh:               fresh,
		})
		if out.OK && len(out.Models) > 0 {
			s.storeModelsCache(cacheKey, out)
		}
		return out, nil
	})
	return listed, err
}

func (s *Service) cachedModels(key string) (models.ListResult, bool) {
	s.modelsCacheMu.Lock()
	defer s.modelsCacheMu.Unlock()
	entry, ok := s.modelsCache[key]
	if !ok || time.Now().After(entry.expiresAt) || len(entry.result.Models) == 0 {
		return models.ListResult{}, false
	}
	return entry.result, true
}

func (s *Service) storeModelsCache(key string, result models.ListResult) {
	s.modelsCacheMu.Lock()
	defer s.modelsCacheMu.Unlock()
	if s.modelsCache == nil {
		s.modelsCache = map[string]modelsCacheEntry{}
	}
	s.modelsCache[key] = modelsCacheEntry{result: result, expiresAt: time.Now().Add(modelsCacheTTL)}
}

func (s *Service) invalidateModelsCache() {
	s.modelsCacheMu.Lock()
	s.modelsCache = map[string]modelsCacheEntry{}
	s.modelsCacheMu.Unlock()
}

// ProbeUpstream 按号池里有凭证的区域做廉价可达性探测（空 POST、无 token）。
// 结果缓存 probeCacheTTL，避免把健康检查打成上游洪水。
func (s *Service) probeKey(site string) string {
	return config.NormalizeSite(site) + "|" + s.ActiveProduct()
}

func (s *Service) invalidateProbeCache() {
	s.probeMu.Lock()
	s.probeCache = map[string]provider.Reachability{}
	s.probeMu.Unlock()
}

func (s *Service) ProbeUpstream(ctx context.Context, force bool) provider.UpstreamStatus {
	sites := s.credentialedSites()
	if len(sites) == 0 {
		sites = []string{s.ActivePoolSite()}
	}
	out := provider.UpstreamStatus{CheckedAt: time.Now().UnixMilli(), Sites: map[string]provider.Reachability{}}
	anyOK := false
	allOK := true
	for _, site := range sites {
		item := s.probeSite(ctx, site, force)
		out.Sites[site] = item
		if item.OK {
			anyOK = true
		} else {
			allOK = false
		}
	}
	out.OK = anyOK
	out.AllOK = allOK && len(sites) > 0
	out.Probed = len(out.Sites) > 0
	if !anyOK && len(out.Sites) > 0 {
		out.Cause = "upstream_infra"
		for _, site := range sites {
			if msg := out.Sites[site].Message; msg != "" {
				out.Message = msg
				out.Cause = out.Sites[site].Cause
				break
			}
		}
	}
	return out
}

func (s *Service) probeSite(ctx context.Context, site string, force bool) provider.Reachability {
	site = config.NormalizeSite(site)
	key := s.probeKey(site)
	if !force {
		s.probeMu.Lock()
		cached, ok := s.probeCache[key]
		s.probeMu.Unlock()
		if ok && cached.CheckedAt > 0 && time.Now().UnixMilli()-cached.CheckedAt < probeCacheTTL.Milliseconds() {
			return cached
		}
	}
	product := s.ActiveProduct()
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	item := s.Provider.ProbeChat(probeCtx, site, product)
	s.probeMu.Lock()
	if s.probeCache == nil {
		s.probeCache = map[string]provider.Reachability{}
	}
	s.probeCache[key] = item
	s.probeMu.Unlock()
	return item
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
	return s.persistPoolRouting(site, s.ActiveProduct(), "号池默认区域已切换到 "+config.NormalizeSite(site)+"。请求仍可用 Key 绑定、X-Site 或 cn:/global: 前缀覆盖。")
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
	s.invalidateProbeCache()
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

func (s *Service) cachedUpstreamStatus() provider.UpstreamStatus {
	s.probeMu.Lock()
	defer s.probeMu.Unlock()
	out := provider.UpstreamStatus{Sites: map[string]provider.Reachability{}}
	if len(s.probeCache) == 0 {
		return out
	}
	product := s.ActiveProduct()
	anyOK := false
	allOK := true
	var latest int64
	for key, item := range s.probeCache {
		cacheSite, cacheProduct, ok := strings.Cut(key, "|")
		if !ok || cacheProduct != product {
			continue
		}
		out.Sites[cacheSite] = item
		out.Probed = true
		if item.CheckedAt > latest {
			latest = item.CheckedAt
		}
		if item.OK {
			anyOK = true
		} else {
			allOK = false
			if out.Message == "" {
				out.Message = item.Message
				out.Cause = item.Cause
			}
		}
	}
	out.OK = anyOK
	out.AllOK = allOK && len(out.Sites) > 0
	out.CheckedAt = latest
	return out
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
	upstream := s.cachedUpstreamStatus()
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
		"upstream":    upstream,
		"watch":       s.WatchStatus(),
		"concurrency": s.Concurrency.Summary(),
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

// oauthFor 按站点选 OAuth 客户端：国际站走专用海外出口，其余沿用默认客户端。
// 没有配置出口时一律回退默认，保证国内站行为与之前完全一致。
func (s *Service) oauthFor(site string) *oauth.Client {
	if s == nil {
		return nil
	}
	if config.NormalizeSite(site) == "global" && s.oauthGlobal != nil {
		return s.oauthGlobal
	}
	return s.OAuth
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

	client := s.oauthFor(sessionSite)
	if client == nil {
		client = s.OAuth
	}
	started, err := client.Start(ctx, sessionSite)
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
	client := s.oauthFor(session.Site)
	if client == nil {
		client = s.OAuth
	}
	polled, err := client.Poll(ctx, session.Site, session.AuthState)
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

// otherSite 返回与 site 相对的另一个站点。只有 domestic / global 两站。
func otherSite(site string) string {
	if config.NormalizeSite(site) == "domestic" {
		return "global"
	}
	return "domestic"
}

// siteHasUsableAccount 报告目标站点是否存在「启用 + 有凭据 + 未冷却 + 未超额 + 未低于积分下限」
// 且对 model 可用的号。不做实际选号，只用来避免无意义的跨区
// （否则会把错误信息换成更有误导性的 no accounts for site=xxx）。
func (s *Service) siteHasUsableAccount(site, model string) bool {
	if s == nil || s.Pool == nil {
		return false
	}
	store, err := s.Pool.Read()
	if err != nil {
		return false
	}
	now := time.Now().UnixMilli()
	floor := s.Pool.QuotaFloor()
	want := config.NormalizeSite(site)
	for _, item := range store.Accounts {
		if config.NormalizeSite(item.Site) != want {
			continue
		}
		if !accounts.UsableForModel(item, now, model, floor) {
			continue
		}
		return true
	}
	return false
}

// routeExhausted 判断一次选号失败是否属于「源站点真的用尽了」。
//
// 只有这三类算：源站点一个能用的号都没有（含全在积分下限下）、
// 以及该模型整体还在上游限流窗口内。其余错误（站点不匹配、内部错误）
// 不该触发跨区 —— 跨过去也只是把错误换成更难排查的上游 429。
func routeExhausted(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, accounts.ErrNoAccounts) || errors.Is(err, accounts.ErrQuotaBelowFloor) {
		return true
	}
	_, cooling := errors.AsType[*accounts.ModelCooldownError](err)
	return cooling
}

// advanceRouteFallback 推进 always 模式规则的备用链（chain[1:]）。
// 与 advanceDeferredRoute 语义一致，只是链来源不同。
func (s *Service) advanceRouteFallback(opts *CompleteOptions) bool {
	if opts == nil || len(opts.routeFallbacks) == 0 {
		return false
	}
	from := config.NormalizeSite(s.requestSite(*opts))
	for len(opts.routeFallbacks) > 0 {
		rule := opts.routeFallbacks[0]
		opts.routeFallbacks = opts.routeFallbacks[1:]
		target := strutil.First(strings.TrimSpace(rule.Target), opts.Model)
		nextSite := config.NormalizeSite(strutil.First(strings.TrimSpace(rule.Site), from))
		if nextSite == from && target == opts.Model {
			continue
		}
		if nextSite != from && !s.siteHasUsableAccount(nextSite, target) {
			s.Log.Warn("route fallback skipped: target site has no usable account",
				"fromSite", from, "toSite", nextSite, "target", target)
			continue
		}
		if nextSite != from {
			opts.ExcludeIDs = nil
		}
		opts.Model = target
		opts.Site = nextSite
		opts.AccountID = ""
		opts.RetryDepth++
		s.Log.Warn("model route fallback applied",
			"fromSite", from, "toSite", nextSite, "target", target)
		return true
	}
	return false
}

// advanceDeferredRoute 在判定「源站点用尽」后推进 mode=onExhausted 的路由链。
//
// 返回 false 表示没有可跳的下一跳：没配路由、链已走完，或目标站点确实没有
// 能接住这个模型的号。最后一种情况跳过 —— 跨区过去只会拿到 429，
// 还把一次请求的额度白烧掉。
func (s *Service) advanceDeferredRoute(opts *CompleteOptions) bool {
	if opts == nil || len(opts.deferredRoute) == 0 {
		return false
	}
	from := config.NormalizeSite(s.requestSite(*opts))
	for len(opts.deferredRoute) > 0 {
		rule := opts.deferredRoute[0]
		opts.deferredRoute = opts.deferredRoute[1:]
		target := strutil.First(strings.TrimSpace(rule.Target), opts.Model)
		nextSite := config.NormalizeSite(strutil.First(strings.TrimSpace(rule.Site), from))
		if nextSite == from && target == opts.Model {
			continue // 空跳：站点和模型都没变
		}
		if nextSite != from && !s.siteHasUsableAccount(nextSite, target) {
			s.Log.Warn("model route skipped: target site has no usable account",
				"fromSite", from, "toSite", nextSite, "target", target)
			continue
		}
		if opts.RoutedFrom == "" {
			opts.RoutedFrom = opts.Model
		}
		// 跨区时清空排除列表：目标站是另一批号，源站的失败记录没有意义。
		// 同站替身必须保留：刚失败的号还在同一批候选里，清空会立刻重打它。
		if nextSite != from {
			opts.ExcludeIDs = nil
		}
		opts.Model = target
		opts.Site = nextSite
		opts.AccountID = ""
		opts.RetryDepth++
		s.Log.Warn("model route applied after source exhausted",
			"fromSite", from, "toSite", nextSite, "target", target)
		return true
	}
	return false
}

// isRetryable500 只把已知的瞬时 500（invoke model error）纳入换号重试，
// 其余 500 视为持续性错误直接返回，避免无差别换号放大请求。
func isRetryable500(msg string) bool {
	return strings.Contains(msg, "invoke model error")
}

// OAuthHTTPFor 返回该站点 OAuth 使用的 HTTP 客户端；未配置专用出口时返回 nil。
func (s *Service) OAuthHTTPFor(site string) *http.Client {
	if s != nil && config.NormalizeSite(site) == "global" && s.oauthGlobal != nil {
		return s.oauthGlobal.HTTP
	}
	return nil
}

// RefreshHTTPFor preserves the dedicated global OAuth egress for all token refreshes.
func (s *Service) RefreshHTTPFor(account accounts.Account) *http.Client {
	if client := s.OAuthHTTPFor(strutil.First(account.Site, s.Config().Site)); client != nil {
		return client
	}
	return s.Provider.For(account.Proxy)
}

// isQuotaExhausted 判断失败是否属于「这个账号额度用尽」类。
// 只认额度/限流：换号确实有用；其余错误换号只是烧配额。
func isQuotaExhausted(err error) bool {
	if err == nil {
		return false
	}
	if billing.IsQuotaExhaustedError(err) || billing.IsModelRateLimitError(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "429") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "6004") ||
		strings.Contains(msg, "quota")
}

// nextSelectableAccount 在指定站点/模型上找一个「真正能被选中」的账号。
// 找不到就返回 false —— 调用方据此返回冷却提示而不是继续打上游。
//
// 判据必须与 Pool.Select 一致（走 accounts.UsableForModel）：以前这里只查
// 冷却，漏了额度耗尽和积分下限，会挑中一个必失败的号白打一次上游。
func (s *Service) nextSelectableAccount(site, model string, excludeIDs []string) (string, bool) {
	if s == nil || s.Pool == nil {
		return "", false
	}
	store, err := s.Pool.Read()
	if err != nil {
		return "", false
	}
	now := time.Now().UnixMilli()
	floor := s.Pool.QuotaFloor()
	exclude := make(map[string]struct{}, len(excludeIDs))
	for _, id := range excludeIDs {
		exclude[id] = struct{}{}
	}
	for _, account := range store.Accounts {
		if config.NormalizeSite(account.Site) != config.NormalizeSite(site) {
			continue
		}
		if _, skip := exclude[account.ID]; skip {
			continue
		}
		if !accounts.UsableForModel(account, now, model, floor) {
			continue
		}
		return account.ID, true
	}
	return "", false
}
