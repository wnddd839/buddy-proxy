package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
)

// models 端点必须接受 site 参数，并把它透传到上游（两站模型集不同）。
func TestModelsEndpointAcceptsSite(t *testing.T) {
	for _, site := range []string{"domestic", "global"} {
		srv, _, _ := checkinTestServer(t, []accounts.Account{checkinAccount("a1")})
		rec := doAdminGet(t, srv, "/direct-admin/api/codebuddy/models?fresh=0&site="+site)
		// 账号凭据是假的，上游会失败；关键是不能 404、不能忽略 site。
		// 期望 200 或 502（上游失败），但响应体里必须能看到 site 语义。
		if rec.Code == http.StatusNotFound {
			t.Fatalf("site=%s 时端点不该 404", site)
		}
		if rec.Code == http.StatusBadGateway {
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("site=%s 错误响应应是 JSON: %v", site, err)
			}
			if got, _ := body["site"].(string); got != site {
				t.Fatalf("site=%s 错误响应应回显 site，got %q", site, got)
			}
		}
	}
}

// 不传 site 时应回落到当前号池站点，不报错也不 404。
func TestModelsEndpointDefaultsToActiveSite(t *testing.T) {
	srv, _, _ := checkinTestServer(t, []accounts.Account{checkinAccount("a1")})
	rec := doAdminGet(t, srv, "/direct-admin/api/codebuddy/models?fresh=0")
	if rec.Code == http.StatusNotFound || rec.Code == http.StatusBadRequest {
		t.Fatalf("缺省 site 不该失败，got %d", rec.Code)
	}
}

// 非法 site 应被归一化（不认识的归 global），而不是报错。
func TestModelsEndpointNormalizesUnknownSite(t *testing.T) {
	srv, _, _ := checkinTestServer(t, []accounts.Account{checkinAccount("a1")})
	rec := doAdminGet(t, srv, "/direct-admin/api/codebuddy/models?fresh=0&site=garbage")
	if rec.Code == http.StatusNotFound || rec.Code == http.StatusBadRequest {
		t.Fatalf("未知 site 应归一化而不是报错，got %d", rec.Code)
	}
}

var _ = httptest.NewRecorder
