// Package accountimport 把多种来源的账号凭据文本转换成账号池条目。
//
// 支持的输入（自动识别，可混在一起）：
//
//  1. 本程序的账号池文件：{"version":1,"provider":"codebuddy","accounts":[...]}
//  2. 纯数组：[{...},{...}] 或单个对象 {...}
//  3. CodeBuddy 客户端凭据：[{"uid":"…","nickname":"…","domain":"www.codebuddy.cn",
//     "access_token":"…","refresh_token":"…","expires_at":1795951021000}]
//  4. 本程序的账号条目本身：{"bearerToken":"…","refreshToken":"…","site":"domestic",…}
//  5. account + auth 包装：{"account":{"uid":"…"},"auth":{"accessToken":"…","refreshToken":"…"}}
//
// 解析只做结构翻译与规范化，不校验 token 是否有效（那是上游的事）。
package accountimport

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/oauth"
	"github.com/wnddd839/codebuddy-proxy/internal/strutil"
)

// ErrNoAccounts 表示文本里没有识别出任何账号。
var ErrNoAccounts = errors.New("没有从文本中识别出账号")

// Options 覆盖推断结果。
type Options struct {
	// Site 非空时强制使用该站点（domestic / global）。
	Site string
	// Label 非空时作为标签前缀，实际标签为 "Label · <昵称>"。
	Label string
}

// Parse 解析文本并返回规范化后的账号（已按凭据去重）。
func Parse(raw []byte, opts Options) ([]accounts.Account, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, ErrNoAccounts
	}
	var root any
	if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
		return nil, fmt.Errorf("不是合法 JSON：%w", err)
	}

	candidates := collect(root)
	out := make([]accounts.Account, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, item := range candidates {
		account, ok := toAccount(item, opts)
		if !ok {
			continue
		}
		key := strutil.First(account.CredentialHash, account.ID)
		if key != "" {
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
		}
		out = append(out, account)
	}
	if len(out) == 0 {
		return nil, ErrNoAccounts
	}
	return out, nil
}

// collect 展平成候选对象列表：剥掉 store 外壳，接受数组/单对象。
func collect(root any) []map[string]any {
	switch v := root.(type) {
	case []any:
		var out []map[string]any
		for _, item := range v {
			out = append(out, collect(item)...)
		}
		return out
	case map[string]any:
		if inner, ok := v["accounts"]; ok {
			// 账号池文件外壳：只取 accounts 数组，忽略 version/provider 等元数据。
			if list, ok := inner.([]any); ok {
				var out []map[string]any
				for _, item := range list {
					out = append(out, collect(item)...)
				}
				return out
			}
		}
		if _, ok := v["auth"]; ok {
			return []map[string]any{v}
		}
		return []map[string]any{v}
	default:
		return nil
	}
}

// toAccount 把单个候选对象翻译成规范化账号。
func toAccount(item map[string]any, opts Options) (accounts.Account, bool) {
	// 已是本程序的账号条目：带 bearerToken/apiKey + authStatus 时直接复用。
	if isNativeAccount(item) {
		raw, err := json.Marshal(item)
		if err != nil {
			return accounts.Account{}, false
		}
		var account accounts.Account
		if err := json.Unmarshal(raw, &account); err != nil {
			return accounts.Account{}, false
		}
		return finalize(account, opts), true
	}

	account := pickAccountBlock(item)
	auth := pickMap(item, "auth")

	bearer := strutil.First(
		str(auth, "accessToken", "access_token", "bearerToken", "bearer_token", "token"),
		str(item, "accessToken", "access_token", "bearerToken", "bearer_token"),
	)
	refresh := strutil.First(
		str(auth, "refreshToken", "refresh_token"),
		str(item, "refreshToken", "refresh_token"),
	)
	if bearer == "" && refresh == "" {
		return accounts.Account{}, false
	}

	domain := strutil.First(
		str(auth, "domain"),
		str(item, "domain", "baseUrl", "base_url"),
	)
	site := strutil.First(
		str(item, "site"),
		str(auth, "realm"),
		inferSiteFromDomain(domain),
	)

	expires := firstInt(
		num(auth, "expiresAt", "expires_at"),
		num(item, "expiresAt", "expires_at", "tokenExpiresAt", "token_expires_at"),
	)
	// 秒/毫秒混用：本程序统一用毫秒。
	if expires > 0 && expires < 1_000_000_000_000 {
		expires *= 1000
	}

	uid := strutil.First(str(account, "uid", "id", "userId"), str(item, "uid", "userId"))
	nickname := strutil.First(str(account, "nickname", "name"), str(item, "nickname", "name"))

	// token 里带更权威的身份信息，优先用它。
	claims := oauth.DecodeJWT(bearer)
	if v := strutil.First(claim(claims, "sub"), claim(claims, "preferred_username")); v != "" {
		uid = strutil.First(uid, v)
	}
	if v := strutil.First(claim(claims, "nickname"), claim(claims, "name"), claim(claims, "preferred_username")); v != "" {
		nickname = strutil.First(nickname, v)
	}
	if exp, ok := claims["exp"].(float64); ok && expires == 0 {
		expires = int64(exp) * 1000
	}

	loggedIn := true
	raw := accounts.Account{
		BearerToken:    bearer,
		RefreshToken:   refresh,
		TokenExpiresAt: expires,
		Domain:         domain,
		Site:           site,
		Source:         "manual_import",
		Transport:      strutil.First(str(item, "transport")),
		AuthStatus: accounts.AuthStatus{
			LoggedIn:     &loggedIn,
			UserID:       uid,
			UserName:     nickname,
			UserNickname: nickname,
			AuthMode:     "token_import",
		},
	}
	return finalize(raw, opts), true
}

func finalize(account accounts.Account, opts Options) accounts.Account {
	if site := strings.TrimSpace(opts.Site); site != "" {
		account.Site = site
	}
	normalized := accounts.CreateAccount(account)
	if prefix := strings.TrimSpace(opts.Label); prefix != "" {
		normalized.Label = prefix + " · " + strutil.First(normalized.AuthStatus.UserNickname, normalized.AuthStatus.UserID, normalized.ID)
	}
	return normalized
}

// isNativeAccount 判断对象是否已经是本程序的账号条目（而不是外部凭据快照）。
func isNativeAccount(item map[string]any) bool {
	_, hasBearer := item["bearerToken"]
	_, hasAPIKey := item["apiKey"]
	_, hasAuthStatus := item["authStatus"]
	_, hasAuth := item["auth"]
	return (hasBearer || hasAPIKey) && hasAuthStatus && !hasAuth
}

func pickAccountBlock(item map[string]any) map[string]any {
	if inner, ok := item["account"].(map[string]any); ok {
		return inner
	}
	return map[string]any{}
}

func pickMap(item map[string]any, key string) map[string]any {
	if inner, ok := item[key].(map[string]any); ok {
		return inner
	}
	return map[string]any{}
}

func str(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key].(string); ok {
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func num(m map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch v := m[key].(type) {
		case float64:
			return int64(v)
		case int64:
			return v
		case json.Number:
			if n, err := v.Int64(); err == nil {
				return n
			}
		}
	}
	return 0
}

func firstInt(values ...int64) int64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func claim(claims map[string]any, key string) string {
	if v, ok := claims[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// inferSiteFromDomain 按域名判断区域，与上游站点一一对应。
func inferSiteFromDomain(domain string) string {
	host := strings.ToLower(strings.TrimSpace(domain))
	switch {
	case host == "":
		return ""
	case strings.Contains(host, "codebuddy.cn"), strings.Contains(host, "workbuddy.cn"):
		return "domestic"
	case strings.Contains(host, "codebuddy.ai"), strings.Contains(host, "workbuddy.ai"), strings.Contains(host, "copilot.tencent.com"):
		return "global"
	default:
		return ""
	}
}
