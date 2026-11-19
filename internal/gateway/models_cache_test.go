package gateway

import (
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/models"
)

func TestStoreModelsCacheDropsExpiredEntries(t *testing.T) {
	// 锁住本次修复：读路径只判断 TTL 不删除，缓存键又含 account.ID，
	// 账号一换就永久留一条 —— 必须在写入时顺手清掉。
	svc := &Service{}
	svc.modelsCache = map[string]modelsCacheEntry{
		"expired": {expiresAt: time.Now().Add(-time.Minute)},
		"live":    {expiresAt: time.Now().Add(time.Minute)},
	}

	svc.storeModelsCache("fresh", models.ListResult{})

	if _, ok := svc.modelsCache["expired"]; ok {
		t.Fatal("过期条目未被清理")
	}
	if _, ok := svc.modelsCache["live"]; !ok {
		t.Fatal("未过期条目被误删")
	}
	if _, ok := svc.modelsCache["fresh"]; !ok {
		t.Fatal("新条目未写入")
	}
}
