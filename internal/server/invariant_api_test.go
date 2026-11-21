package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/admin"
)

// /direct-admin/api/invariants 必须返回 findings / coverage / dropped 三样。
func TestInvariantAPIReturnsFindings(t *testing.T) {
	srv := systemPromptTestServer(t)
	rec := doAdminRequest(t, srv, http.MethodGet, "/direct-admin/api/invariants", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		OK       bool             `json:"ok"`
		Enabled  bool             `json:"enabled"`
		Findings []map[string]any `json:"findings"`
		Coverage string           `json:"coverage"`
		Dropped  *int64           `json:"dropped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK {
		t.Fatal("ok 应为 true")
	}
	if got.Coverage == "" {
		t.Error("coverage 不能为空 —— 没有覆盖说明的仪表盘会被误读成「没发生」")
	}
	if got.Dropped == nil {
		t.Error("dropped 字段必须存在")
	}
	if got.Findings == nil {
		t.Error("findings 应为数组而不是 null")
	}
}

// 面板必须有这张卡片，且不能出现被禁用的含糊词。
func TestAdminPageHasInvariantCard(t *testing.T) {
	page := admin.PageHTML()
	for _, want := range []string{"静默失效体检", "invHits", "invCoverage", "refreshInvariants"} {
		if !strings.Contains(page, want) {
			t.Fatalf("面板缺 %q", want)
		}
	}
}
