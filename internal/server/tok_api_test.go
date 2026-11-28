package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/decision"
	"github.com/wnddd839/codebuddy-proxy/internal/gateway"
	"github.com/wnddd839/codebuddy-proxy/internal/invariant"
	"github.com/wnddd839/codebuddy-proxy/internal/tokfp"
)

// /direct-admin/api/tok-baselines 必须返回结构化的基线行，
// 且未经观测时是空数组而不是 null（前端要能直接 .map）。
func TestTokBaselinesAPI(t *testing.T) {
	srv, _ := tokBaselineTestServer(t)

	rec := doAdminRequest(t, srv, http.MethodGet, "/direct-admin/api/tok-baselines", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		OK        bool             `json:"ok"`
		Baselines []map[string]any `json:"baselines"`
		Count     int              `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK {
		t.Fatal("ok 应为 true")
	}
	if got.Baselines == nil {
		t.Fatal("baselines 应为数组而不是 null")
	}
	if got.Count != len(got.Baselines) {
		t.Fatalf("count(%d) 与数组长度(%d) 不一致", got.Count, len(got.Baselines))
	}
}

// 灌入 3 个样本后必须出现一行基线，且带快/慢/当前/样本四值。
func TestTokBaselinesAPIShowsObservedBaseline(t *testing.T) {
	srv, sink := tokBaselineTestServer(t)

	for i := 0; i < 3; i++ {
		sink.Submit(decision.Record{
			Kind: decision.KindRequest, At: time.Now().UnixMilli(),
			Model: "deepseek-v4.1-flash", Ok: true,
			TokBucket: tokfp.BucketDigits, TokRatio: 0.4552, TokChars: 6600,
		})
	}

	rec := doAdminRequest(t, srv, http.MethodGet, "/direct-admin/api/tok-baselines", "")
	var got struct {
		Baselines []map[string]any `json:"baselines"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Baselines) != 1 {
		t.Fatalf("应有 1 行基线，实际 %d：%+v", len(got.Baselines), got.Baselines)
	}
	row := got.Baselines[0]
	if row["model"] != "deepseek-v4.1-flash" || row["bucket"] != tokfp.BucketDigits {
		t.Fatalf("%+v", row)
	}
	for _, k := range []string{"fast", "slow", "lastRatio", "samples"} {
		if row[k] == nil {
			t.Errorf("缺字段 %q：%+v", k, row)
		}
	}
}

func tokBaselineTestServer(t *testing.T) (*Server, *invariant.Sink) {
	t.Helper()
	dir := t.TempDir()
	if os.Getenv("CODEBUDDY_PROXY_ENV_FILE") == "" {
		t.Setenv("CODEBUDDY_PROXY_ENV_FILE", filepath.Join(dir, ".env"))
	}
	store, err := decision.OpenStore(dir, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sink := invariant.NewSink(store, invariant.DefaultRegistry(), invariant.NewState(64, 64), 50, 128)
	sink.Start()
	t.Cleanup(sink.Stop)

	cfg := config.Config{AccountsPath: filepath.Join(dir, "proxy-accounts.json")}
	svc := gateway.New(cfg, nil)
	t.Cleanup(func() { _ = svc.Close() })
	svc.SetInvariants(sink)
	srv := New(cfg, svc)
	return srv, sink
}
