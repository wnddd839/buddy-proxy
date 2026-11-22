package server

import (
	"net/http"
	"sync"
	"testing"
)

// /direct-admin/api/invariants 的 specs 必须稳定且与 DefaultRegistry 同源。
//
// 之前每次请求都新建一次 DefaultRegistry()：既浪费，又埋下
// 「面板展示的清单」与「实际跑的清单」将来分叉的隐患。
func TestInvariantSpecsAreCachedAndConsistent(t *testing.T) {
	srv := systemPromptTestServer(t)
	rec := doAdminRequest(t, srv, http.MethodGet, "/direct-admin/api/invariants", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d", rec.Code)
	}

	a := invariantSpecs()
	b := invariantSpecs()
	if len(a) == 0 {
		t.Fatal("specs 不应为空")
	}
	if len(a) != len(b) {
		t.Fatalf("两次调用长度不同：%d vs %d", len(a), len(b))
	}

	// 并发调用也必须稳定（once 保护）。
	var wg sync.WaitGroup
	results := make([][]map[string]string, 8)
	for i := range results {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = invariantSpecs()
		}(i)
	}
	wg.Wait()
	for i := range results {
		if len(results[i]) != len(a) {
			t.Fatalf("并发第 %d 次长度不同：%d vs %d", i, len(results[i]), len(a))
		}
	}

	// 每条都要有 id/title/why，否则面板 tooltip 会是空的。
	for _, spec := range a {
		if spec["id"] == "" || spec["title"] == "" || spec["why"] == "" {
			t.Errorf("spec 不完整：%+v", spec)
		}
	}
}
