package concurrency

import (
	"testing"
	"time"
)

func TestTrackerPrunesExpiredBucketsOnWrite(t *testing.T) {
	// 锁住本次修复：秒桶必须在写入路径上被清理，不能只依赖 Snapshot()。
	// 回归表现是「没人打开管理台 ⇒ 每秒每账号留一个桶 ⇒ 无限增长」。
	tr := New()

	tr.mu.Lock()
	st := tr.locked("acc-old")
	oldKey := time.Now().Add(-2 * time.Hour).Unix()
	st.buckets[oldKey] = &bucket{requests: 7}

	fresh := tr.locked("acc-live")
	freshKey := time.Now().Unix()
	fresh.buckets[freshKey] = &bucket{requests: 3}

	// 让下一次写入必然触发清理
	tr.lastPrune = time.Now().Add(-10 * time.Minute)
	tr.mu.Unlock()

	finish := tr.Begin("acc-new", "")
	finish(true)

	tr.mu.Lock()
	defer tr.mu.Unlock()

	if st, ok := tr.accounts["acc-old"]; ok {
		if _, still := st.buckets[oldKey]; still {
			t.Fatal("过期秒桶没有被清理")
		}
		if st.inFlight == 0 && len(st.buckets) == 0 {
			t.Fatal("空账号条目应一并删除")
		}
	}

	live, ok := tr.accounts["acc-live"]
	if !ok {
		t.Fatal("活跃账号被误删")
	}
	if _, ok := live.buckets[freshKey]; !ok {
		t.Fatal("未过期的秒桶被误删")
	}
}

func TestTrackerPruneRespectsInterval(t *testing.T) {
	// 清理不能每个请求都做一次全表扫描：间隔内的写入不应触发。
	tr := New()
	tr.mu.Lock()
	st := tr.locked("acc-1")
	oldKey := time.Now().Add(-2 * time.Hour).Unix()
	st.buckets[oldKey] = &bucket{requests: 1}
	tr.lastPrune = time.Now() // 刚清理过
	tr.mu.Unlock()

	finish := tr.Begin("acc-1", "")
	finish(true)

	tr.mu.Lock()
	defer tr.mu.Unlock()
	if _, ok := tr.accounts["acc-1"].buckets[oldKey]; !ok {
		t.Fatal("清理间隔内不应扫描全表")
	}
}

func TestTrackerSnapshotStillPrunes(t *testing.T) {
	tr := New()
	tr.mu.Lock()
	st := tr.locked("acc-1")
	st.buckets[time.Now().Add(-3*time.Hour).Unix()] = &bucket{requests: 1}
	tr.mu.Unlock()

	_ = tr.Snapshot()

	tr.mu.Lock()
	defer tr.mu.Unlock()
	for _, st := range tr.accounts {
		for key := range st.buckets {
			if key < time.Now().Add(-time.Hour).Unix() {
				t.Fatal("Snapshot 后仍有过期桶")
			}
		}
	}
}
