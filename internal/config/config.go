package config

import (
	"crypto/subtle"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/watch"
)

const (
	// DefaultShutdownGrace 覆盖慢思考 + 长上下文的在途回合。
	DefaultShutdownGrace       = 10 * time.Minute
	DefaultHost                = "127.0.0.1"
	DefaultPort                = 32126
	DefaultChatCompletionsPath = "/v2/chat/completions"
	DefaultTransport           = "protocol_direct"
	DefaultIDEVersion          = "2.117.2"
	DefaultWorkBuddyIDEVersion = "1.119.0"
	DefaultWorkBuddyProductVer = "4.9.29177644"
	DefaultRefreshWindow       = 10 * time.Minute
	DefaultStreamKeepAlive     = 5 * time.Second
	DefaultOAuthSessionTTL     = 15 * time.Minute
	DefaultHTTPTimeout         = 120 * time.Second
	DefaultServerIdleTimeout   = 120 * time.Second
	// StallTimeout 是上游流「连续无字节」看门狗：首字节前用它，命中后切到
	// StallTimeoutAfterFirstByte。两者都远大于正常思考间隔，避免长静默被误杀。
	// 默认值从 3 分钟提到 10 分钟（用户实测有 5~6 分钟的长思考回合）。
	DefaultStallTimeout               = 10 * time.Minute
	DefaultStallTimeoutAfterFirstByte = 10 * time.Minute
	DefaultMaxBodyMB                  = 64
	// 签到失败重试：10 分钟后重试一次。签到失败多是上游瞬时抖动，
	// 试太多次意义有限；间隔太短则可能撞上同一波故障。
	DefaultCheckinRetryInterval = 10 * time.Minute
	DefaultCheckinRetryMax      = 1

	// SystemPromptMode* 控制 system 提示词的处理策略。
	// auto = 原样优先，命中 11128 时自动降级 fold；keep = 永远原样；fold = 永远折叠。
	SystemPromptModeAuto = "auto"
	SystemPromptModeKeep = "keep"
	SystemPromptModeFold = "fold"

	DefaultIdleConnTimeout     = 90 * time.Second
	DefaultMaxIdleConns        = 100
	DefaultMaxIdleConnsPerHost = 20
)

// APIKeyBinding 将网关 Key 绑定到区域或具体账号；账号绑定的区域由账号推导。
type APIKeyBinding struct {
	Key       string
	Site      string
	AccountID string
}

type Config struct {
	Host          string
	Port          int
	APIKey        string
	APIKeys       []APIKeyBinding
	AdminPassword string
	RequireAPIKey bool
	PublicBaseURL string
	AccountsPath  string
	// FailureDumpDir is an opt-in directory for sensitive upstream request dumps.
	FailureDumpDir      string
	UsagePath           string
	BaseURL             string
	Site                string
	Product             string
	InternetEnvironment string
	APIEndpoint         string
	ChatCompletionsPath string
	Transport           string
	IDEVersion          string
	RefreshWindow       time.Duration
	StreamKeepAlive     time.Duration
	OAuthSessionTTL     time.Duration
	// OAuthProxy 是国际站（global）OAuth 专用出口，形如 socks5h://127.0.0.1:17998。
	// 为空则沿用默认客户端（国内站路径不变）。
	// 存在的理由：www.codebuddy.ai 从中国大陆直连会被 TLS 握手超时拦掉，
	// 而 OAuth 的 auth/state 与 auth/token 两个调用都走这个客户端。
	OAuthProxy  string
	HTTPTimeout time.Duration
	// StallTimeout 首字节前的「连续无字节」阈值；0 表示关闭看门狗（仅测试用）。
	StallTimeout time.Duration
	// StallTimeoutAfterFirstByte 收到任意 SSE 增量后的阈值（含 thinking/心跳）。
	// 真正的「上游假活」几乎都发生在首字节之前，之后放宽以免掐断长思考。
	StallTimeoutAfterFirstByte time.Duration
	ServerIdleTimeout          time.Duration
	ShutdownGrace              time.Duration
	MaxBodyMB                  int
	MaxIdleConns               int
	MaxIdleConnsPerHost        int
	IdleConnTimeout            time.Duration
	DefaultModels              []string

	// Watch 是进程内波动监测配置（见 internal/watch）。
	Watch watch.Config
	// CheckinAuto 是每账号自动签到总开关（默认关闭，见 internal/checkin）。
	CheckinAuto bool
	// CheckinAllowDirectFallback 允许自动签到在账号绑定出口失败时回落直连重试一次。
	CheckinAllowDirectFallback bool
	// CheckinRetryInterval 是签到失败后多久再试一次（0 = 不重试）。
	// 重试状态从签到日志推导，不存额外状态文件，所以进程重启后仍能继续。
	CheckinRetryInterval time.Duration
	// CheckinRetryMax 是当天最多重试几次（不含首次尝试，默认 1）。
	CheckinRetryMax int
	// SystemPromptMode 是 system 提示词处理策略，取值 auto / keep / fold。
	SystemPromptMode string
	// QuotaFloorCredits 是账号积分低于该值时不再参与选号（0 = 不启用）。
	// 数据源是缓存的 billing credits（QuotaRemaining），与配额耗尽同一口径。
	QuotaFloorCredits float64
}

func Load() Config {
	// 尽力加载：二进制用户期望 exe/cwd 旁的 .env 自动生效。
	_, _ = LoadDotEnv()

	cfg := Config{
		Host:                       envOr("CODEBUDDY_PROXY_HOST", "CURSOR_DIRECT_HOST", DefaultHost),
		Port:                       envInt("CODEBUDDY_PROXY_PORT", "CURSOR_DIRECT_PORT", DefaultPort),
		APIKey:                     firstEnv("CODEBUDDY_PROXY_API_KEY", "CURSOR_DIRECT_API_KEY", "CURSOR_GATEWAY_API_KEY"),
		APIKeys:                    ParseAPIKeys(firstEnv("CODEBUDDY_PROXY_API_KEYS")),
		AdminPassword:              firstEnv("CODEBUDDY_PROXY_ADMIN_PASSWORD", "CURSOR_DIRECT_ADMIN_PASSWORD", "CURSOR_GATEWAY_ADMIN_PASSWORD"),
		PublicBaseURL:              strings.TrimRight(firstEnv("CODEBUDDY_PROXY_PUBLIC_BASE_URL", "CURSOR_DIRECT_PUBLIC_BASE_URL"), "/"),
		FailureDumpDir:             firstEnv("CODEBUDDY_PROXY_DUMP_FAILURES"),
		AccountsPath:               expandHome(envOr("CODEBUDDY_PROXY_ACCOUNTS_PATH", "CURSOR_DIRECT_CODEBUDDY_ACCOUNTS_PATH", defaultAccountsPath())),
		Site:                       strings.ToLower(firstEnv("CODEBUDDY_SITE", "CURSOR_DIRECT_CODEBUDDY_SITE")),
		Product:                    strings.ToLower(firstEnv("CODEBUDDY_PRODUCT")),
		InternetEnvironment:        strings.ToLower(firstEnv("CODEBUDDY_INTERNET_ENVIRONMENT", "CURSOR_DIRECT_CODEBUDDY_INTERNET_ENVIRONMENT")),
		APIEndpoint:                strings.TrimRight(firstEnv("CODEBUDDY_API_ENDPOINT", "CURSOR_DIRECT_CODEBUDDY_API_ENDPOINT"), "/"),
		ChatCompletionsPath:        envOr("CODEBUDDY_CHAT_COMPLETIONS_PATH", "CURSOR_DIRECT_CODEBUDDY_CHAT_COMPLETIONS_PATH", DefaultChatCompletionsPath),
		Transport:                  DefaultTransport,
		IDEVersion:                 envOr("CODEBUDDY_IDE_VERSION", "", DefaultIDEVersion),
		RefreshWindow:              envDuration("CODEBUDDY_REFRESH_WINDOW_MS", "CURSOR_DIRECT_CODEBUDDY_REFRESH_WINDOW_MS", DefaultRefreshWindow),
		StreamKeepAlive:            envDuration("CODEBUDDY_PROXY_STREAM_KEEPALIVE_MS", "CURSOR_DIRECT_STREAM_KEEPALIVE_MS", DefaultStreamKeepAlive),
		OAuthSessionTTL:            DefaultOAuthSessionTTL,
		OAuthProxy:                 strings.TrimSpace(firstEnv("CODEBUDDY_PROXY_OAUTH_PROXY")),
		HTTPTimeout:                DefaultHTTPTimeout,
		StallTimeout:               envDurationPositive("CODEBUDDY_PROXY_STALL_TIMEOUT_MS", DefaultStallTimeout),
		StallTimeoutAfterFirstByte: envDurationPositive("CODEBUDDY_PROXY_STALL_AFTER_FIRST_BYTE_MS", DefaultStallTimeoutAfterFirstByte),
		ServerIdleTimeout:          envDuration("CODEBUDDY_PROXY_SERVER_IDLE_TIMEOUT_MS", "", DefaultServerIdleTimeout),
		ShutdownGrace:              envDuration("CODEBUDDY_PROXY_SHUTDOWN_GRACE_MS", "", DefaultShutdownGrace),
		MaxBodyMB:                  envInt("CODEBUDDY_PROXY_MAX_BODY_MB", "", DefaultMaxBodyMB),
		MaxIdleConns:               DefaultMaxIdleConns,
		MaxIdleConnsPerHost:        DefaultMaxIdleConnsPerHost,
		IdleConnTimeout:            DefaultIdleConnTimeout,
		DefaultModels:              []string{"auto"},
		Watch:                      loadWatchConfig(),
		CheckinAuto:                envBool("CODEBUDDY_PROXY_CHECKIN_AUTO", "", false),
		CheckinAllowDirectFallback: envBool("CODEBUDDY_PROXY_CHECKIN_ALLOW_DIRECT_FALLBACK", "", false),
		CheckinRetryInterval:       envDuration("CODEBUDDY_PROXY_CHECKIN_RETRY_INTERVAL_MS", "", DefaultCheckinRetryInterval),
		CheckinRetryMax:            envInt("CODEBUDDY_PROXY_CHECKIN_RETRY_MAX", "", DefaultCheckinRetryMax),
		QuotaFloorCredits:          envFloat("CODEBUDDY_PROXY_QUOTA_FLOOR_CREDITS", 0),
		SystemPromptMode:           NormalizeSystemPromptMode(firstEnv("CODEBUDDY_PROXY_SYSTEM_PROMPT_MODE")),
	}

	// 管理台密码为空则开放管理页（本地友好）。
	// /v1 API Key 门禁仍由 CODEBUDDY_PROXY_REQUIRE_API_KEY 独立控制。
	cfg.RequireAPIKey = envBool("CODEBUDDY_PROXY_REQUIRE_API_KEY", "CURSOR_DIRECT_REQUIRE_API_KEY", cfg.APIKey != "" || len(cfg.APIKeys) > 0)
	if usageEnv := strings.TrimSpace(firstEnv("CODEBUDDY_PROXY_USAGE_PATH")); usageEnv != "" {
		cfg.UsagePath = expandHome(usageEnv)
	} else {
		cfg.UsagePath = DefaultUsagePath(cfg.AccountsPath)
	}
	cfg.Site = NormalizeSite(cfg.Site)
	cfg.Product = NormalizeProduct(cfg.Product)
	cfg.BaseURL = envOr("CODEBUDDY_BASE_URL", "CURSOR_DIRECT_CODEBUDDY_BASE_URL", resolveDefaultBaseURL(cfg.Site, cfg.InternetEnvironment, cfg.Product))
	if raw := firstEnv("CODEBUDDY_PROXY_MODELS", "CURSOR_DIRECT_CODEBUDDY_MODELS"); raw != "" {
		parts := strings.Split(raw, ",")
		cfg.DefaultModels = cfg.DefaultModels[:0]
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				cfg.DefaultModels = append(cfg.DefaultModels, part)
			}
		}
		if len(cfg.DefaultModels) == 0 {
			cfg.DefaultModels = []string{"auto"}
		}
	}
	return cfg
}

func resolveDefaultBaseURL(site, internetEnvironment, product string) string {
	if NormalizeProduct(product) == "workbuddy" {
		return ProductPortalBaseURL(site, product)
	}
	env := strings.ToLower(strings.TrimSpace(internetEnvironment))
	site = strings.ToLower(strings.TrimSpace(site))
	if env == "internal" || env == "ioa" {
		return "https://copilot.tencent.com"
	}
	if site == "domestic" || site == "cn" || site == "china" || env == "domestic" || env == "cn" || env == "china" {
		return "https://www.codebuddy.cn"
	}
	return "https://www.codebuddy.ai"
}

func defaultAccountsPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", "proxy-accounts.json")
	}
	return filepath.Join(home, ".codebuddy", "proxy-accounts.json")
}

// DefaultUsagePath 与账号池同目录，默认 proxy-usage.json。
func DefaultUsagePath(accountsPath string) string {
	accountsPath = strings.TrimSpace(accountsPath)
	if accountsPath == "" {
		accountsPath = defaultAccountsPath()
	}
	return filepath.Join(filepath.Dir(accountsPath), "proxy-usage.json")
}

func expandHome(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return defaultAccountsPath()
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, rest)
	}
	if rest, ok := strings.CutPrefix(path, `~\`); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, rest)
	}
	return path
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if key == "" {
			continue
		}
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func envOr(primary, fallback, def string) string {
	if value := firstEnv(primary, fallback); value != "" {
		return value
	}
	return def
}

func envInt(primary, fallback string, def int) int {
	raw := firstEnv(primary, fallback)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

func envBool(primary, fallback string, def bool) bool {
	raw := strings.ToLower(firstEnv(primary, fallback))
	if raw == "" {
		return def
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func envDuration(primary, fallback string, def time.Duration) time.Duration {
	raw := firstEnv(primary, fallback)
	if raw == "" {
		return def
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return def
	}
	return time.Duration(n) * time.Millisecond
}

// envDurationPositive 读毫秒配置，只在严格为正时采用；空 / 非法 / 非正都回落默认。
// 用于看门狗阈值：0 会关闭看门狗，绝不能由误配置悄悄产生。
func envDurationPositive(primary string, def time.Duration) time.Duration {
	raw := firstEnv(primary)
	if raw == "" {
		return def
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 || n > math.MaxInt64/int64(time.Millisecond) {
		return def
	}
	return time.Duration(n) * time.Millisecond
}

func (c Config) Addr() string {
	return c.Host + ":" + strconv.Itoa(c.Port)
}

// ParseAPIKeys 解析区域或 account=<id> 后缀；无后缀的 Key 只做鉴权。
func ParseAPIKeys(raw string) []APIKeyBinding {
	out := make([]APIKeyBinding, 0, strings.Count(raw, ",")+1)
	seen := map[string]int{}
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, site, accountID := part, "", ""
		if k, rest, ok := strings.Cut(part, ":"); ok {
			key = strings.TrimSpace(k)
			if id, bound := strings.CutPrefix(strings.TrimSpace(rest), "account="); bound {
				accountID = strings.TrimSpace(id)
				if accountID == "" {
					continue
				}
			} else {
				site = OptionalSite(rest)
			}
		}
		if key == "" {
			continue
		}
		binding := APIKeyBinding{Key: key, Site: site, AccountID: accountID}
		if i, ok := seen[key]; ok {
			out[i] = binding
			continue
		}
		seen[key] = len(out)
		out = append(out, binding)
	}
	return out
}

// FormatAPIKeys 将区域或账号绑定写回兼容的环境变量语法。
func FormatAPIKeys(keys []APIKeyBinding) string {
	parts := make([]string, 0, len(keys))
	for _, binding := range keys {
		key := strings.TrimSpace(binding.Key)
		if key == "" {
			continue
		}
		if binding.AccountID != "" {
			parts = append(parts, key+":account="+binding.AccountID)
			continue
		}
		if site := OptionalSite(binding.Site); site != "" {
			parts = append(parts, key+":"+site)
			continue
		}
		parts = append(parts, key)
	}
	return strings.Join(parts, ",")
}

// LookupAPIKey 校验网关 Key。ok 表示鉴权通过；site 非空表示这把 Key 绑定了区域。
func (c Config) LookupAPIKey(token string) (ok bool, site string) {
	ok, binding := c.LookupAPIKeyBinding(token)
	return ok, binding.Site
}

// LookupAPIKeyBinding 返回完整绑定约束，主 Key 同样允许配置账号绑定。
func (c Config) LookupAPIKeyBinding(token string) (bool, APIKeyBinding) {
	if token == "" {
		return false, APIKeyBinding{}
	}
	matched := false
	var result APIKeyBinding
	for _, binding := range c.APIKeys {
		if secretEqual(token, binding.Key) {
			matched = true
			result = binding
		}
	}
	if secretEqual(token, c.APIKey) {
		matched = true
	}
	return matched, result
}

func secretEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func NormalizeSite(value string) string {
	text := strings.ToLower(strings.TrimSpace(value))
	switch text {
	case "domestic", "cn", "china", "internal":
		return "domestic"
	default:
		// 空 / international / global / 未知 → 归为全球
		return "global"
	}
}

// OptionalSite 空值保持为空，避免把「未指定」归一成 global。
func OptionalSite(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return NormalizeSite(value)
}

// NormalizeSystemPromptMode 把任意输入归一为三位合法模式之一。
// 未知值回落 auto（默认行为：原样优先，命中 11128 才降级）。
func NormalizeSystemPromptMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case SystemPromptModeKeep, "preserve", "passthrough", "1", "true":
		return SystemPromptModeKeep
	case SystemPromptModeFold, "0", "false":
		return SystemPromptModeFold
	default:
		return SystemPromptModeAuto
	}
}

// IsValidSystemPromptMode 判断是否为精确的合法模式值（不做归一）。
// 管理台写入前用它拒绝垃圾输入，避免把拼写错误静默成 auto。
func IsValidSystemPromptMode(value string) bool {
	switch value {
	case SystemPromptModeAuto, SystemPromptModeKeep, SystemPromptModeFold:
		return true
	default:
		return false
	}
}

// SiteModelPrefix 是模型 ID 上的区域标签（cn / global），供按请求路由。
func SiteModelPrefix(site string) string {
	if NormalizeSite(site) == "domestic" {
		return "cn"
	}
	return "global"
}

// SplitSitePrefix 解析 `cn:model` / `global:model`。无区域前缀时 ok=false。
func SplitSitePrefix(model string) (site, rest string, ok bool) {
	cleaned := strings.TrimSpace(model)
	key, after, found := strings.Cut(cleaned, ":")
	if !found {
		return "", cleaned, false
	}
	rest = strings.TrimSpace(after)
	if rest == "" {
		rest = "auto"
	}
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "cn", "domestic", "china", "internal":
		return "domestic", rest, true
	case "global", "intl", "international":
		return "global", rest, true
	default:
		return "", cleaned, false
	}
}

func NormalizeProduct(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "workbuddy", "wb", "ide":
		return "workbuddy"
	default:
		return "codebuddy"
	}
}

func ProductPortalBaseURL(site, product string) string {
	if NormalizeProduct(product) == "workbuddy" {
		if NormalizeSite(site) == "domestic" {
			return "https://www.workbuddy.cn"
		}
		return "https://www.workbuddy.ai"
	}
	if NormalizeSite(site) == "domestic" {
		return "https://www.codebuddy.cn"
	}
	return "https://www.codebuddy.ai"
}

// envPresent 报告环境变量是否被显式设置（哪怕值为空也算「用户动过」）。
// 用于区分「零值 = 未配置」与「用户明确填了 false」。
func envPresent(keys ...string) bool {
	for _, key := range keys {
		if key == "" {
			continue
		}
		if _, ok := os.LookupEnv(key); ok {
			return true
		}
	}
	return false
}
