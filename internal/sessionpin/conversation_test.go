package sessionpin_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/sessionpin"
)

func TestConversationIDStableWithinTTL(t *testing.T) {
	table := sessionpin.NewConversationTable(45 * time.Minute)
	first := table.ID("hdr:conv-1\x1ecodebuddy", "account-a")
	if first == "" {
		t.Fatal("expected non-empty conversation id")
	}
	for range 3 {
		if got := table.ID("hdr:conv-1\x1ecodebuddy", "account-a"); got != first {
			t.Fatalf("conversation id changed within ttl: %s vs %s", got, first)
		}
	}
}

func TestConversationIDFormat(t *testing.T) {
	table := sessionpin.NewConversationTable(0)
	id := table.ID("hdr:conv", "account-a")
	parts := strings.Split(id, "-")
	if len(parts) != 5 {
		t.Fatalf("uuid shape: %q", id)
	}
	if len(parts[0]) != 8 || len(parts[1]) != 4 || len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		t.Fatalf("uuid field widths: %q", id)
	}
	if id[14] != '4' {
		t.Fatalf("expected version nibble 4 (RFC 4122 v4), got %q", id)
	}
	if c := id[19]; c != '8' && c != '9' && c != 'a' && c != 'b' {
		t.Fatalf("expected variant nibble 8/9/a/b, got %q", id)
	}
}

func TestConversationIDDifferentKeysAndAccounts(t *testing.T) {
	table := sessionpin.NewConversationTable(0)
	base := table.ID("hdr:conv-1\x1ecodebuddy", "account-a")
	if base == table.ID("hdr:conv-2\x1ecodebuddy", "account-a") {
		t.Fatal("different sessions must not share an upstream conversation id")
	}
	if base == table.ID("hdr:conv-1\x1ecodebuddy", "account-b") {
		t.Fatal("account switch must mint a new upstream conversation id")
	}
	if table.ID("hdr:conv-1\x1eproduct-a", "account-a") == table.ID("hdr:conv-1\x1eproduct-b", "account-a") {
		t.Fatal("different products on one session must not share an id")
	}
}

func TestConversationIDRotatesAfterAccountSwitchAndBack(t *testing.T) {
	table := sessionpin.NewConversationTable(time.Hour)
	original := table.ID("key", "account-a")
	if got := table.ID("key", "account-b"); got == original {
		t.Fatal("account switch must rotate the id")
	}
	if got := table.ID("key", "account-a"); got == original {
		t.Fatal("returning to the old account must mint a fresh id, not resurrect the stale one")
	}
}

func TestConversationIDRotatesAfterIdleExpiry(t *testing.T) {
	table := sessionpin.NewConversationTable(time.Millisecond)
	original := table.ID("key", "account-a")
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := table.ID("key", "account-a"); got != original {
			return // 过期后轮换成功
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("expired entry must rotate to a fresh id")
}

func TestConversationIDIsConcurrencySafe(t *testing.T) {
	table := sessionpin.NewConversationTable(0)
	const workers = 16
	ids := make([]string, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() {
			ids[i] = table.ID("shared-key", "account-a")
		})
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] {
			t.Fatal("parallel first requests for one session must share one id")
		}
	}
}

func TestConversationIDEmptyKeyOrAccountFallsBack(t *testing.T) {
	table := sessionpin.NewConversationTable(0)
	if got := table.ID("", "account-a"); got != "" {
		t.Fatalf("empty key must return empty id, got %q", got)
	}
	if got := table.ID("key", " "); got != "" {
		t.Fatalf("blank account must return empty id, got %q", got)
	}
	var nilTable *sessionpin.ConversationTable
	if got := nilTable.ID("key", "account-a"); got != "" {
		t.Fatalf("nil table must return empty id, got %q", got)
	}
}

func TestConversationTableEvictsOldestAtCapacity(t *testing.T) {
	table := sessionpin.NewConversationTable(time.Hour)
	const capacity = 4096
	first := table.ID("filler-0", "account-a") // 最早插入，之后不再触碰
	time.Sleep(5 * time.Millisecond)           // 拉开到期时间，避免计时精度平局
	for i := 1; i < capacity; i++ {
		table.ID(fmt.Sprintf("filler-%d", i), "account-a")
	}
	table.ID("overflow-key", "account-a") // 容量已满：淘汰到期最早（最早插入）的 filler-0
	if got := table.ID("filler-0", "account-a"); got == first {
		t.Fatal("oldest entry should have been evicted at capacity, id must rotate")
	}
}
