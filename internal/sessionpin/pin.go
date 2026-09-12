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

// Key 从客户端头或对话前缀得到稳定 session id。
// 有头/prompt_cache_key 时用客户端值；否则用 system+首条 user，后续工具轮次不会换 key。
func Key(header http.Header, promptCacheKey string, messages []map[string]any) string {
	if header != nil {
		for _, name := range []string{
			"X-Session-Id",
			"X-Opencode-Session",
			"X-Conversation-Id",
			"Conversation-Id",
			"Session-Id",
		} {
			if v := strings.TrimSpace(header.Get(name)); v != "" {
				return "hdr:" + v
			}
		}
	}
	if v := strings.TrimSpace(promptCacheKey); v != "" {
		return "hdr:" + v
	}
	prefix := conversationPrefix(messages)
	if prefix == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(prefix))
	return "msg:" + hex.EncodeToString(sum[:8])
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
