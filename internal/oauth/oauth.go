package oauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/strutil"
)

const (
	pluginAuthStatePath        = "/v2/plugin/auth/state"
	pluginAuthTokenPath        = "/v2/plugin/auth/token"
	pluginAuthTokenRefreshPath = "/v2/plugin/auth/token/refresh"
)

type Client struct {
	HTTP *http.Client

	mu              sync.Mutex
	lastPluginState string
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{HTTP: httpClient}
}

func ResolvePluginBaseURL(site string) string {
	if config.NormalizeSite(site) == "domestic" {
		return "https://www.codebuddy.cn"
	}
	return "https://www.codebuddy.ai"
}

type StartResult struct {
	OK             bool   `json:"ok"`
	BaseURL        string `json:"baseUrl"`
	Site           string `json:"site"`
	AuthState      string `json:"authState"`
	AuthURL        string `json:"authUrl"`
	TokenEndpoint  string `json:"tokenEndpoint"`
	ExpiresIn      int    `json:"expiresIn"`
	PollIntervalMs int    `json:"pollIntervalMs"`
}

type TokenData struct {
	BearerToken      string `json:"bearerToken"`
	AccessToken      string `json:"accessToken"`
	TokenType        string `json:"tokenType"`
	ExpiresIn        int64  `json:"expiresIn"`
	RefreshExpiresIn int64  `json:"refreshExpiresIn"`
	RefreshToken     string `json:"refreshToken"`
	SessionState     string `json:"sessionState"`
	Scope            string `json:"scope"`
	Domain           string `json:"domain"`
}

type PollResult struct {
	Status    string         `json:"status"`
	Message   string         `json:"message"`
	Code      any            `json:"code,omitempty"`
	TokenData *TokenData     `json:"tokenData,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

func (c *Client) Start(ctx context.Context, site string) (StartResult, error) {
	baseURL := provider.NormalizeBaseURL(ResolvePluginBaseURL(site))
	nonce := strutil.RandomHex(8)
	authState, authURL, err := c.requestAuthState(ctx, baseURL, nonce)
	if err != nil {
		return StartResult{}, err
	}
	c.mu.Lock()
	if c.lastPluginState != "" && authState == c.lastPluginState {
		c.mu.Unlock()
		retryNonce := strutil.RandomHex(8)
		retryState, retryURL, retryErr := c.requestAuthState(ctx, baseURL, retryNonce)
		if retryErr == nil && retryState != "" && retryState != authState {
			authState, authURL = retryState, retryURL
		}
		c.mu.Lock()
	}
	c.lastPluginState = authState
	c.mu.Unlock()
	return StartResult{
		OK:             true,
		BaseURL:        baseURL,
		Site:           config.NormalizeSite(site),
		AuthState:      authState,
		AuthURL:        authURL,
		TokenEndpoint:  fmt.Sprintf("%s%s?state=%s", baseURL, pluginAuthTokenPath, url.QueryEscape(authState)),
		ExpiresIn:      1800,
		PollIntervalMs: 5000,
	}, nil
}

func (c *Client) requestAuthState(ctx context.Context, baseURL, nonce string) (string, string, error) {
	endpoint := fmt.Sprintf("%s%s?platform=CLI&nonce=%s", baseURL, pluginAuthStatePath, nonce)
	body, _ := json.Marshal(map[string]string{"nonce": nonce})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header = buildStartHeaders(baseURL)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	payload, _ := readJSON(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := json.Marshal(payload)
		return "", "", fmt.Errorf("CodeBuddy auth/state failed with %d: %s", resp.StatusCode, strutil.Truncate(string(raw), 200))
	}
	code, _ := payload["code"].(float64)
	data, _ := payload["data"].(map[string]any)
	if int(code) != 0 || data == nil {
		msg, _ := payload["msg"].(string)
		if msg == "" {
			msg = fmt.Sprintf("CodeBuddy auth/state error (code %v)", payload["code"])
		}
		return "", "", fmt.Errorf("%s", msg)
	}
	authState := strings.TrimSpace(fmt.Sprint(data["state"]))
	authURL := strings.TrimSpace(fmt.Sprint(data["authUrl"]))
	if authState == "" || authURL == "" {
		return "", "", fmt.Errorf("CodeBuddy auth/state returned empty state or authUrl")
	}
	return authState, authURL, nil
}

func (c *Client) Poll(ctx context.Context, site, authState string) (PollResult, error) {
	authState = strings.TrimSpace(authState)
	if authState == "" {
		return PollResult{Status: "error", Message: "missing auth state"}, nil
	}
	baseURL := provider.NormalizeBaseURL(ResolvePluginBaseURL(site))
	endpoint := fmt.Sprintf("%s%s?state=%s", baseURL, pluginAuthTokenPath, url.QueryEscape(authState))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return PollResult{}, err
	}
	req.Header = buildPollHeaders(baseURL)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return PollResult{}, err
	}
	defer resp.Body.Close()
	payload, _ := readJSON(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return PollResult{Status: "error", Message: fmt.Sprintf("token poll HTTP %d", resp.StatusCode), Payload: payload}, nil
	}
	if isPending(payload) {
		return PollResult{
			Status:  "pending",
			Message: strutil.First(fmt.Sprint(payload["msg"]), "waiting for login"),
			Code:    payload["code"],
		}, nil
	}
	if token := extractTokenData(payload); token != nil {
		return PollResult{Status: "success", Message: "authenticated", TokenData: token, Payload: payload}, nil
	}
	return PollResult{
		Status:  "unknown",
		Message: strutil.First(fmt.Sprint(payload["msg"]), fmt.Sprint(payload["message"]), fmt.Sprintf("unknown auth status (code %v)", payload["code"])),
		Code:    payload["code"],
		Payload: payload,
	}, nil
}

type RefreshOptions struct {
	Site            string
	BaseURL         string
	AccessToken     string
	RefreshToken    string
	RefreshEndpoint string
	// HTTPClient 可指定账号自己的出口（nil 用默认客户端）。
	// 绑了出口的账号连刷新令牌也走同一 IP，避免同一账号在两个 IP 之间跳。
	HTTPClient *http.Client
}

func (c *Client) Refresh(ctx context.Context, opts RefreshOptions) (*TokenData, error) {
	refreshToken := strings.TrimSpace(opts.RefreshToken)
	if refreshToken == "" {
		return nil, fmt.Errorf("CodeBuddy credential has no refresh token")
	}
	baseURL := provider.NormalizeBaseURL(strutil.First(opts.BaseURL, ResolvePluginBaseURL(opts.Site)))
	endpoint := strings.TrimRight(strings.TrimSpace(opts.RefreshEndpoint), "/")
	if endpoint == "" {
		endpoint = baseURL + pluginAuthTokenRefreshPath
		if strings.HasSuffix(baseURL, "/v2") {
			endpoint = baseURL + pluginAuthTokenRefreshPath[3:]
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	req.Header = buildRefreshHeaders(endpoint, opts.AccessToken, refreshToken)
	httpClient := c.HTTP
	if opts.HTTPClient != nil {
		httpClient = opts.HTTPClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	payload, _ := readJSON(resp.Body)
	token := extractTokenData(payload)
	code, _ := payload["code"].(float64)
	if (resp.StatusCode < 200 || resp.StatusCode >= 300) || (token == nil && code != 0) {
		msg := strutil.First(fmt.Sprint(payload["error_description"]), fmt.Sprint(payload["error"]), fmt.Sprint(payload["msg"]), fmt.Sprint(payload["message"]), fmt.Sprintf("HTTP %d", resp.StatusCode))
		return nil, fmt.Errorf("CodeBuddy token refresh failed with %d: %s", resp.StatusCode, strutil.Truncate(msg, 200))
	}
	if token == nil || token.BearerToken == "" {
		msg := strutil.First(fmt.Sprint(payload["msg"]), fmt.Sprint(payload["message"]), "no access token")
		return nil, fmt.Errorf("CodeBuddy token refresh returned no access token: %s", strutil.Truncate(msg, 200))
	}
	if token.RefreshToken == "" {
		token.RefreshToken = refreshToken
	}
	return token, nil
}

func ShouldRefresh(account accounts.Account, force bool, window time.Duration, now time.Time) bool {
	if strings.TrimSpace(account.RefreshToken) == "" {
		return false
	}
	if force {
		return true
	}
	if account.TokenExpiresAt <= 0 {
		if claims := DecodeJWT(account.BearerToken); claims != nil {
			if exp, ok := asInt64(claims["exp"]); ok && exp > 0 {
				account.TokenExpiresAt = exp * 1000
			}
		}
	}
	if account.TokenExpiresAt <= 0 {
		return false
	}
	return time.UnixMilli(account.TokenExpiresAt).Sub(now) <= window
}

func AccountFromTokenData(token *TokenData, site, label string) accounts.Account {
	bearer := strings.TrimSpace(token.BearerToken)
	if bearer == "" {
		bearer = strings.TrimSpace(token.AccessToken)
	}
	claims := DecodeJWT(bearer)
	userID := strutil.First(fmt.Sprint(claims["email"]), fmt.Sprint(claims["preferred_username"]), fmt.Sprint(claims["sub"]))
	userName := strutil.First(fmt.Sprint(claims["name"]), fmt.Sprint(claims["preferred_username"]), fmt.Sprint(claims["email"]), userID)
	loggedIn := true
	createdAt := time.Now().Unix()
	expiresAt := int64(0)
	if token.ExpiresIn > 0 {
		expiresAt = createdAt*1000 + token.ExpiresIn*1000
	} else if exp, ok := asInt64(claims["exp"]); ok {
		expiresAt = exp * 1000
	}
	return accounts.CreateAccount(accounts.Account{
		Label:          strutil.First(label, userName, userID, "CodeBuddy"),
		Enabled:        true,
		Source:         "cli_credential",
		Site:           config.NormalizeSite(site),
		Transport:      config.DefaultTransport,
		BearerToken:    bearer,
		RefreshToken:   strings.TrimSpace(token.RefreshToken),
		TokenExpiresAt: expiresAt,
		Domain:         strings.TrimSpace(token.Domain),
		AuthStatus: accounts.AuthStatus{
			LoggedIn:     new(loggedIn),
			UserID:       userID,
			UserName:     userName,
			UserNickname: strutil.First(fmt.Sprint(claims["nickname"]), fmt.Sprint(claims["name"])),
			AuthMode:     "cli_oauth",
		},
	})
}

func DecodeJWT(token string) map[string]any {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) < 2 {
		return map[string]any{}
	}
	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	raw, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		raw, err = base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return map[string]any{}
		}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func buildStartHeaders(baseURL string) http.Header {
	domain := hostOf(baseURL)
	requestID := strutil.RandomHex(16)
	h := http.Header{}
	h.Set("Accept", "application/json, text/plain, */*")
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-cache")
	h.Set("Pragma", "no-cache")
	h.Set("Connection", "close")
	h.Set("X-Requested-With", "XMLHttpRequest")
	h.Set("X-Domain", domain)
	h.Set("X-No-Authorization", "true")
	h.Set("X-No-User-Id", "true")
	h.Set("X-No-Enterprise-Id", "true")
	h.Set("X-No-Department-Info", "true")
	h.Set("User-Agent", "CLI/1.0.8 CodeBuddy/1.0.8")
	h.Set("X-Product", "SaaS")
	h.Set("X-Request-Id", requestID)
	return h
}

func buildPollHeaders(baseURL string) http.Header {
	domain := hostOf(baseURL)
	requestID := strutil.RandomHex(16)
	spanID := strutil.RandomHex(4)
	h := http.Header{}
	h.Set("Accept", "application/json, text/plain, */*")
	h.Set("Cache-Control", "no-cache")
	h.Set("Pragma", "no-cache")
	h.Set("Connection", "close")
	h.Set("X-Requested-With", "XMLHttpRequest")
	h.Set("X-Request-Id", requestID)
	h.Set("b3", fmt.Sprintf("%s-%s-1-", requestID, spanID))
	h.Set("X-B3-Traceid", requestID)
	h.Set("X-B3-Parentspanid", "")
	h.Set("X-B3-Spanid", spanID)
	h.Set("X-B3-Sampled", "1")
	h.Set("X-No-Authorization", "true")
	h.Set("X-No-User-Id", "true")
	h.Set("X-No-Enterprise-Id", "true")
	h.Set("X-No-Department-Info", "true")
	h.Set("X-Domain", domain)
	h.Set("User-Agent", "CLI/1.0.8 CodeBuddy/1.0.8")
	h.Set("X-Product", "SaaS")
	return h
}

func buildRefreshHeaders(endpoint, accessToken, refreshToken string) http.Header {
	domain := hostOf(endpoint)
	requestID := strutil.RandomHex(16)
	spanID := strutil.RandomHex(4)
	h := http.Header{}
	h.Set("Accept", "application/json, text/plain, */*")
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-cache")
	h.Set("Pragma", "no-cache")
	h.Set("Connection", "close")
	h.Set("X-Requested-With", "XMLHttpRequest")
	h.Set("X-Request-Id", requestID)
	h.Set("b3", fmt.Sprintf("%s-%s-1-", requestID, spanID))
	h.Set("X-B3-Traceid", requestID)
	h.Set("X-B3-Parentspanid", "")
	h.Set("X-B3-Spanid", spanID)
	h.Set("X-B3-Sampled", "1")
	h.Set("X-Domain", domain)
	h.Set("X-Refresh-Token", refreshToken)
	h.Set("X-Auth-Refresh-Source", "plugin")
	h.Set("User-Agent", "CLI/1.0.8 CodeBuddy/1.0.8")
	h.Set("X-Product", "SaaS")
	if token := strings.TrimSpace(accessToken); token != "" {
		h.Set("Authorization", "Bearer "+token)
	}
	return h
}

func extractTokenData(payload map[string]any) *TokenData {
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		data = payload
	}
	access := strutil.First(
		fmt.Sprint(data["accessToken"]),
		fmt.Sprint(data["access_token"]),
		fmt.Sprint(data["token"]),
		fmt.Sprint(payload["accessToken"]),
		fmt.Sprint(payload["access_token"]),
		findBearerDeep(payload, 0),
	)
	if access == "" {
		return nil
	}
	return &TokenData{
		BearerToken:      access,
		AccessToken:      access,
		TokenType:        strutil.First(fmt.Sprint(data["tokenType"]), fmt.Sprint(data["token_type"]), "Bearer"),
		ExpiresIn:        asInt64Default(data["expiresIn"], data["expires_in"]),
		RefreshExpiresIn: asInt64Default(data["refreshExpiresIn"], data["refresh_expires_in"]),
		RefreshToken:     strutil.First(fmt.Sprint(data["refreshToken"]), fmt.Sprint(data["refresh_token"]), fmt.Sprint(payload["refreshToken"]), fmt.Sprint(payload["refresh_token"])),
		SessionState:     strutil.First(fmt.Sprint(data["sessionState"]), fmt.Sprint(data["session_state"])),
		Scope:            strings.TrimSpace(fmt.Sprint(data["scope"])),
		Domain:           strings.TrimSpace(fmt.Sprint(data["domain"])),
	}
}

func findBearerDeep(value any, depth int) string {
	if depth > 8 || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		text := strings.TrimSpace(v)
		if strings.HasPrefix(text, "eyJ") && strings.Count(text, ".") >= 2 {
			return text
		}
	case map[string]any:
		for _, key := range []string{"accessToken", "access_token", "bearerToken", "bearer_token", "token"} {
			if found := findBearerDeep(v[key], depth+1); found != "" {
				return found
			}
		}
		for _, nested := range v {
			if found := findBearerDeep(nested, depth+1); found != "" {
				return found
			}
		}
	}
	return ""
}

func isPending(payload map[string]any) bool {
	code, _ := payload["code"].(float64)
	msg := strings.ToLower(strutil.First(fmt.Sprint(payload["msg"]), fmt.Sprint(payload["message"])))
	return int(code) == 11217 || strings.Contains(msg, "login") && strings.Contains(msg, "ing") || strings.Contains(msg, "waiting")
}

func readJSON(r io.Reader) (map[string]any, error) {
	raw, err := io.ReadAll(io.LimitReader(r, 2<<20))
	if err != nil {
		return map[string]any{}, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{"raw": string(raw)}, nil
	}
	return out, nil
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		trimmed := raw
		if v, ok := strings.CutPrefix(trimmed, "https://"); ok {
			trimmed = v
		} else if v, ok := strings.CutPrefix(trimmed, "http://"); ok {
			trimmed = v
		}
		if i := strings.IndexByte(trimmed, '/'); i >= 0 {
			return trimmed[:i]
		}
		return trimmed
	}
	return u.Host
}

func asInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	default:
		return 0, false
	}
}

func asInt64Default(values ...any) int64 {
	for _, value := range values {
		if n, ok := asInt64(value); ok {
			return n
		}
	}
	return 0
}
