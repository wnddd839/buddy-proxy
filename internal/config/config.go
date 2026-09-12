package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
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
	DefaultIdleConnTimeout     = 90 * time.Second
	DefaultMaxIdleConns        = 100
	DefaultMaxIdleConnsPerHost = 20
)

type Config struct {
	Host                string
	Port                int
	APIKey              string
	AdminPassword       string
	RequireAPIKey       bool
	PublicBaseURL       string
	AccountsPath        string
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
	HTTPTimeout         time.Duration
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
	DefaultModels       []string
}

func Load() Config {
	// 尽力加载：二进制用户期望 exe/cwd 旁的 .env 自动生效。
	_, _ = LoadDotEnv()

	cfg := Config{
		Host:                envOr("CODEBUDDY_PROXY_HOST", "CURSOR_DIRECT_HOST", DefaultHost),
		Port:                envInt("CODEBUDDY_PROXY_PORT", "CURSOR_DIRECT_PORT", DefaultPort),
		APIKey:              firstEnv("CODEBUDDY_PROXY_API_KEY", "CURSOR_DIRECT_API_KEY", "CURSOR_GATEWAY_API_KEY"),
		AdminPassword:       firstEnv("CODEBUDDY_PROXY_ADMIN_PASSWORD", "CURSOR_DIRECT_ADMIN_PASSWORD", "CURSOR_GATEWAY_ADMIN_PASSWORD"),
		PublicBaseURL:       strings.TrimRight(firstEnv("CODEBUDDY_PROXY_PUBLIC_BASE_URL", "CURSOR_DIRECT_PUBLIC_BASE_URL"), "/"),
		AccountsPath:        expandHome(envOr("CODEBUDDY_PROXY_ACCOUNTS_PATH", "CURSOR_DIRECT_CODEBUDDY_ACCOUNTS_PATH", defaultAccountsPath())),
		Site:                strings.ToLower(firstEnv("CODEBUDDY_SITE", "CURSOR_DIRECT_CODEBUDDY_SITE")),
		Product:             strings.ToLower(firstEnv("CODEBUDDY_PRODUCT")),
		InternetEnvironment: strings.ToLower(firstEnv("CODEBUDDY_INTERNET_ENVIRONMENT", "CURSOR_DIRECT_CODEBUDDY_INTERNET_ENVIRONMENT")),
		APIEndpoint:         strings.TrimRight(firstEnv("CODEBUDDY_API_ENDPOINT", "CURSOR_DIRECT_CODEBUDDY_API_ENDPOINT"), "/"),
		ChatCompletionsPath: envOr("CODEBUDDY_CHAT_COMPLETIONS_PATH", "CURSOR_DIRECT_CODEBUDDY_CHAT_COMPLETIONS_PATH", DefaultChatCompletionsPath),
		Transport:           DefaultTransport,
		IDEVersion:          envOr("CODEBUDDY_IDE_VERSION", "", DefaultIDEVersion),
		RefreshWindow:       envDuration("CODEBUDDY_REFRESH_WINDOW_MS", "CURSOR_DIRECT_CODEBUDDY_REFRESH_WINDOW_MS", DefaultRefreshWindow),
		StreamKeepAlive:     envDuration("CODEBUDDY_PROXY_STREAM_KEEPALIVE_MS", "CURSOR_DIRECT_STREAM_KEEPALIVE_MS", DefaultStreamKeepAlive),
		OAuthSessionTTL:     DefaultOAuthSessionTTL,
		HTTPTimeout:         DefaultHTTPTimeout,
		MaxIdleConns:        DefaultMaxIdleConns,
		MaxIdleConnsPerHost: DefaultMaxIdleConnsPerHost,
		IdleConnTimeout:     DefaultIdleConnTimeout,
		DefaultModels:       []string{"auto"},
	}

	// 管理台密码为空则开放管理页（本地友好）。
	// /v1 API Key 门禁仍由 CODEBUDDY_PROXY_REQUIRE_API_KEY 独立控制。
	cfg.RequireAPIKey = envBool("CODEBUDDY_PROXY_REQUIRE_API_KEY", "CURSOR_DIRECT_REQUIRE_API_KEY", cfg.APIKey != "")
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

func (c Config) Addr() string {
	return c.Host + ":" + strconv.Itoa(c.Port)
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
