package sessionpin

import (
	"crypto/rand"
	"fmt"
	"strings"
	"sync"
	"time"
)

// conversation 是一条「会话键 → 上游会话 ID」的活跃记录。
type conversation struct {
	accountID string
	id        string
	until     time.Time
}

// ConversationTable 让同一会话、站点、产品与账号复用同一个上游会话 ID
// （X-Conversation-ID），条目随账号钉一起按 TTL 空闲过期。丢了只影响
// 上游 prompt cache 命中，不丢账号文件。
type ConversationTable struct {
	ttl time.Duration
	mu  sync.Mutex
	m   map[string]conversation
}

// NewConversationTable 建 ConversationTable；ttl 非正时回落默认值（45 分钟）。
func NewConversationTable(ttl time.Duration) *ConversationTable {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	return &ConversationTable{ttl: ttl, m: map[string]conversation{}}
}

// ID 返回该会话键与账号当前绑定的上游会话 ID；没有就生成新的。
// 生成在锁内完成，让同一会话的并发首请求共享同一个 ID。
// 键或账号为空时返回空串，调用方回落逐请求随机 ID。
func (t *ConversationTable) ID(key, accountID string) string {
	key = strings.TrimSpace(key)
	accountID = strings.TrimSpace(accountID)
	if t == nil || key == "" || accountID == "" {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lockedID(key, accountID)
}

func (t *ConversationTable) lockedID(key, accountID string) string {
	now := time.Now()
	if entry, ok := t.m[key]; ok {
		if now.Before(entry.until) && entry.accountID == accountID {
			entry.until = now.Add(t.ttl)
			t.m[key] = entry
			return entry.id
		}
		delete(t.m, key)
	}
	if len(t.m) >= maxEntries {
		t.evictExpiredLocked(now)
		if len(t.m) >= maxEntries {
			t.evictOldestLocked()
		}
	}
	entry := conversation{accountID: accountID, id: randomUUIDv4(), until: now.Add(t.ttl)}
	t.m[key] = entry
	return entry.id
}

func (t *ConversationTable) evictExpiredLocked(now time.Time) {
	for k, c := range t.m {
		if now.After(c.until) {
			delete(t.m, k)
		}
	}
}

func (t *ConversationTable) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time
	first := true
	for k, c := range t.m {
		if first || c.until.Before(oldest) {
			oldestKey = k
			oldest = c.until
			first = false
		}
	}
	if oldestKey != "" {
		delete(t.m, oldestKey)
	}
}

// randomUUIDv4 生成 RFC 4122 v4 UUID。随机源失败属于系统级故障：
// 宁可 panic 也不静默发出空/重复会话 ID。
func randomUUIDv4() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(fmt.Sprintf("sessionpin: crypto/rand unavailable: %v", err))
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}
