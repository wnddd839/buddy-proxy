package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// rules / titleRoute 必须「给了才改」：漏传一个字段不能把另一块清空。
// 旧实现是整体写入，只发 titleRoute 会把 rules 写成 nil（静默清空）。
func TestModelRoutesPartialUpdatesPreserveOtherSection(t *testing.T) {
	srv := testServer(t, false, "", "")

	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		return doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/model-routes", body)
	}
	get := func() map[string]any {
		t.Helper()
		rec := doAdminRequest(t, srv, http.MethodGet, "/direct-admin/api/model-routes", "")
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("解析 GET 失败：%v", err)
		}
		return got
	}

	if rec := post(`{"rules":[{"fromSite":"domestic","match":"gpt-5","site":"global","target":"glm-5","mode":"always"}]}`); rec.Code != http.StatusOK {
		t.Fatalf("写 rules 失败：%d %s", rec.Code, rec.Body.String())
	}

	// 只改 titleRoute：rules 必须原地保留。
	if rec := post(`{"titleRoute":{"enabled":true,"domestic":"hy4-flash-free"}}`); rec.Code != http.StatusOK {
		t.Fatalf("写 titleRoute 失败：%d %s", rec.Code, rec.Body.String())
	}
	got := get()
	if rules, _ := got["rules"].([]any); len(rules) != 1 {
		t.Fatalf("只改 titleRoute 把 rules 清掉了：%v", got["rules"])
	}
	tr, _ := got["titleRoute"].(map[string]any)
	if tr == nil || tr["enabled"] != true {
		t.Fatalf("titleRoute 未生效：%v", got["titleRoute"])
	}

	// 只改 rules：titleRoute 必须原地保留。
	if rec := post(`{"rules":[]}`); rec.Code != http.StatusOK {
		t.Fatalf("清空 rules 失败：%d %s", rec.Code, rec.Body.String())
	}
	got = get()
	tr, _ = got["titleRoute"].(map[string]any)
	if tr == nil || tr["enabled"] != true {
		t.Fatalf("只改 rules 把 titleRoute 清掉了：%v", got["titleRoute"])
	}

	// 两个都不给：拒绝，而不是写出一张空表。
	if rec := post(`{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("空 body 应 400，实际 %d", rec.Code)
	}

	// 显式 null 才是清空。
	if rec := post(`{"titleRoute":null}`); rec.Code != http.StatusOK {
		t.Fatalf("显式清空 titleRoute 失败：%d %s", rec.Code, rec.Body.String())
	}
	if got = get(); got["titleRoute"] != nil {
		t.Fatalf("titleRoute 应为 null：%v", got["titleRoute"])
	}
}
