package gateway

import (
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/decision"
	"github.com/wnddd839/codebuddy-proxy/internal/invariant"
)

// 出图 / 搜索路径同样走 Pool.Select，必须把 RequestID 带下去。
//
// 实测（2026-11-22）：这两条路径原先没有 RequestID，于是它们的
// selection 记录 requestId 为空 —— 与 request 记录无法配对，
// 回放时既算不出覆盖率，也看不出这些请求选了谁。
func TestImageAndSearchCarryRequestID(t *testing.T) {
	t.Run("ImageOptions 有 RequestID", func(t *testing.T) {
		o := ImageOptions{RequestID: "r-img"}
		if o.RequestID != "r-img" {
			t.Fatal("ImageOptions 必须能携带 RequestID")
		}
	})
	t.Run("SearchOptions 有 RequestID", func(t *testing.T) {
		o := SearchOptions{RequestID: "r-srch"}
		if o.RequestID != "r-srch" {
			t.Fatal("SearchOptions 必须能携带 RequestID")
		}
	})
}

// 端到端：出图路径的 selection 事件必须带 requestId。
func TestImageSelectEmitsRequestID(t *testing.T) {
	dir := t.TempDir()
	store, err := decision.OpenStore(dir, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sink := invariant.NewSink(store, invariant.DefaultRegistry(), invariant.NewState(64, 64), 50, 128)
	sink.Start()

	svc := New(config.Config{Site: "domestic", AccountsPath: dir + "/accounts.json"}, nil)
	t.Cleanup(func() { _ = svc.Close() })
	svc.SetInvariants(sink)

	acc := accounts.CreateAccount(accounts.Account{
		Label: "a", Site: "domestic", BearerToken: "t", Enabled: true,
	})
	if _, _, err := svc.Pool.Upsert(acc); err != nil {
		t.Fatal(err)
	}

	// 固定账号 + 假 token：流程在 Select 之后立即因上游失败返回，
	// 但 selection 事件已经发出，足以验证 RequestID 是否透传。
	_, err = svc.ImageFromPool(t.Context(), ImageOptions{
		Site: "domestic", RequestID: "r-img-1", FixedAccount: true,
		AccountID: acc.ID, Prompt: "x",
	})
	if err == nil {
		t.Skip("出图意外成功（上游可用），跳过断言")
	}

	sink.Stop()
	recs, err := store.ReadKind(time.Now().Add(-time.Minute), time.Now().Add(time.Minute), decision.KindSelection)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if r.RequestID == "r-img-1" {
			return
		}
	}
	t.Fatalf("出图路径的 selection 必须带 RequestID，实际记录：%+v", recs)
}
