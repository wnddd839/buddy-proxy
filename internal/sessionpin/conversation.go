package sessionpin

import (
	"crypto/rand"
	"fmt"
	"strings"
	"sync"
	"time"
)

type conversation struct {
	accountID string
	id        string
	until     time.Time
}

// ConversationTable keeps an upstream conversation ID stable for an active
// session, site, product and account. Entries expire with the account pins.
type ConversationTable struct {
	ttl time.Duration
	mu  sync.Mutex
	m   map[string]conversation
}

func NewConversationTable(ttl time.Duration) *ConversationTable {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	return &ConversationTable{ttl: ttl, m: map[string]conversation{}}
}

// ID returns one UUID per active key and account. The lock also makes parallel
// first requests for the same session share an ID.
func (t *ConversationTable) ID(key, accountID string) string {
	key = strings.TrimSpace(key)
	accountID = strings.TrimSpace(accountID)
	if t == nil || key == "" || accountID == "" {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.m == nil {
		t.m = map[string]conversation{}
	}
	now := time.Now()
	if entry, ok := t.m[key]; ok {
		if now.Before(entry.until) && entry.accountID == accountID {
			entry.until = now.Add(t.ttl)
			t.m[key] = entry
			return entry.id
		}
		delete(t.m, key)
	}
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return ""
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	id := fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
	if len(t.m) >= maxEntries {
		var oldestKey string
		var oldest time.Time
		for k, entry := range t.m {
			if !now.Before(entry.until) {
				delete(t.m, k)
				continue
			}
			if oldestKey == "" || entry.until.Before(oldest) {
				oldestKey, oldest = k, entry.until
			}
		}
		if len(t.m) >= maxEntries {
			delete(t.m, oldestKey)
		}
	}
	t.m[key] = conversation{accountID: accountID, id: id, until: now.Add(t.ttl)}
	return id
}
