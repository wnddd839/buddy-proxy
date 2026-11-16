// Package sessionpin 把「同一段对话」钉在同一个号池账号上，不改号池轮询本身。
package sessionpin

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

const defaultTTL = 45 * time.Minute
const maxEntries = 4096

// SessionLabel 用首条 user 消息做可读会话名（管理台展示）。
func SessionLabel(messages []map[string]any) string {
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		if strings.ToLower(strings.TrimSpace(role)) != "user" {
			continue
		}
		text := messageText(msg["content"])
		if text == "" {
			continue
		}
		text = strings.Join(strings.Fields(text), " ")
		if len(text) > 48 {
			return text[:48] + "…"
		}
		return text
	}
	prefix := conversationPrefix(messages)
	if prefix == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(prefix))
	return "会话 · " + hex.EncodeToString(sum[:4])
}

// 会话键的来源，用于事件观测（面板可显示客户端有没有发会话头）。
const (
	KeySourceHeader   = "header"   // 命中客户端显式会话头
	KeySourceCacheKey = "cachekey" // 命中请求体 prompt_cache_key
	KeySourceContent  = "content"  // 回落 system+首条 user 内容哈希
	KeySourceNone     = "none"     // 无任何信号
)

// sessionHeaders 是客户端显式会话头的候选列表，按优先级排序。
//
// 各 agent 的专属头（源码实证）：
//   - opencode        : X-Opencode-Session（值形如 ses_<64hex>）
//   - deepseek-harness: X-Deepseek-Harness-Session-Id
//
// 通用约定头（X-Session-Id / X-Conversation-Id / Conversation-Id / Session-Id）
// 排在最前：它们是客户端有意指定的显式值，语义最强。
var sessionHeaders = []string{
	"X-Session-Id",
	"X-Opencode-Session",
	"X-Deepseek-Harness-Session-Id",
	"X-Conversation-Id",
	"Conversation-Id",
	"Session-Id",
}

// Key 从客户端头或对话前缀得到稳定 session id，并返回其来源。
//
// 三级回退：
//  1. 客户端显式会话头（含各 agent 专属头）→ "hdr:<值>"
//  2. 请求体 prompt_cache_key           → "hdr:<值>"
//  3. system + 首条 user 的内容哈希      → "msg:<8字节hex>"
//
// 第 3 级只用「system + 首条 user」，因此同一段对话的后续工具轮次不会换 key。
//
// ⚠️ 调用方必须在**任何 system 改写之前**调用本函数（当前在 server handler
// 里，早于 provider 层剥离）。否则 strip 档把 system 剥空后，
// 不同客户端会算出相同的 msg: 键而互相抢账号钉。
func Key(header http.Header, promptCacheKey string, messages []map[string]any) (string, string) {
	if header != nil {
		for _, name := range sessionHeaders {
			if v := strings.TrimSpace(header.Get(name)); v != "" {
				return "hdr:" + v, KeySourceHeader
			}
		}
	}
	if v := strings.TrimSpace(promptCacheKey); v != "" {
		return "hdr:" + v, KeySourceCacheKey
	}
	prefix := conversationPrefix(messages)
	if prefix == "" {
		return "", KeySourceNone
	}
	sum := sha256.Sum256([]byte(prefix))
	return "msg:" + hex.EncodeToString(sum[:8]), KeySourceContent
}

func conversationPrefix(messages []map[string]any) string {
	var system, user string
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		text := messageText(msg["content"])
		if text == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(role)) {
		case "system":
			if system == "" {
				system = text
			}
		case "user":
			if user == "" {
				user = text
			}
		}
		if user != "" {
			break
		}
	}
	if user == "" && system == "" {
		return ""
	}
	return system + "\n" + user
}

func messageText(content any) string {
	switch v := content.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		var b strings.Builder
		for _, part := range v {
			m, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := m["text"].(string); t != "" {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(t)
			}
		}
		return strings.TrimSpace(b.String())
	default:
		return ""
	}
}

type pin struct {
	accountID string
	until     time.Time
}

// Table 是进程内 session→account 映射。丢了只影响缓存命中，不丢账号文件。
type Table struct {
	ttl time.Duration
	mu  sync.Mutex
	m   map[string]pin
}

func New(ttl time.Duration) *Table {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	return &Table{ttl: ttl, m: map[string]pin{}}
}

func (t *Table) Remember(key, accountID string) {
	key = strings.TrimSpace(key)
	accountID = strings.TrimSpace(accountID)
	if t == nil || key == "" || accountID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.m) >= maxEntries {
		t.evictExpiredLocked(time.Now())
		if len(t.m) >= maxEntries {
			t.evictOldestLocked()
		}
	}
	t.m[key] = pin{accountID: accountID, until: time.Now().Add(t.ttl)}
}

func (t *Table) Lookup(key string) (string, bool) {
	key = strings.TrimSpace(key)
	if t == nil || key == "" {
		return "", false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	p, ok := t.m[key]
	if !ok {
		return "", false
	}
	if time.Now().After(p.until) {
		delete(t.m, key)
		return "", false
	}
	return p.accountID, true
}

func (t *Table) Forget(key string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	delete(t.m, strings.TrimSpace(key))
	t.mu.Unlock()
}

func (t *Table) evictExpiredLocked(now time.Time) {
	for k, p := range t.m {
		if now.After(p.until) {
			delete(t.m, k)
		}
	}
}

func (t *Table) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time
	first := true
	for k, p := range t.m {
		if first || p.until.Before(oldest) {
			oldestKey = k
			oldest = p.until
			first = false
		}
	}
	if oldestKey != "" {
		delete(t.m, oldestKey)
	}
}
