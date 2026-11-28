package accounts

import (
	"testing"
	"time"
)

// 三条选号路径都必须发 SelectionEvent，且带正确的 Source。
//
// 背景（实测）：上线后 45 分钟，55 条 request 只有 8 条 selection ——
// 因为会话软钉走的是「指定账号」分支，那条分支从不发事件。
// 结果不是误报而是盲区：回答不了「多少流量走亲和、多少真轮转」。
func TestAllSelectionPathsEmitWithSource(t *testing.T) {
	t.Run("软钉/指定账号", func(t *testing.T) {
		pool := newObserverPool(t)
		acc := CreateAccount(Account{Label: "a", BearerToken: "t", Site: "global"})
		if _, _, err := pool.Upsert(acc); err != nil {
			t.Fatal(err)
		}
		obs := &fakeObserver{}
		pool.SetObserver(obs)

		sel, err := pool.Select(SelectOptions{
			Site: "global", Model: "m", AccountID: acc.ID, RequestID: "r-pin",
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(obs.selection) != 1 {
			t.Fatalf("指定账号路径也要发事件，实际 %d", len(obs.selection))
		}
		e := obs.selection[0]
		if e.Source != SelSourcePin {
			t.Fatalf("Source 应为 %q，实际 %q", SelSourcePin, e.Source)
		}
		if e.Examined != 1 || e.Chosen != acc.ID || e.RequestID != "r-pin" {
			t.Fatalf("%+v", e)
		}
		if len(e.Skipped) != 0 {
			t.Fatalf("指定账号不检查别人，不该有跳过原因：%v", e.Skipped)
		}
		_ = sel
	})

	t.Run("按额度最大（快路径）", func(t *testing.T) {
		pool := newObserverPool(t)
		rich := 100.0
		acc := CreateAccount(Account{
			Label: "rich", BearerToken: "t", Site: "global",
			QuotaRemaining: &rich, QuotaCheckedAt: time.Now().UnixMilli(),
			QuotaResetAt: time.Now().Add(time.Hour).UnixMilli(),
		})
		if _, _, err := pool.Upsert(acc); err != nil {
			t.Fatal(err)
		}
		obs := &fakeObserver{}
		pool.SetObserver(obs)

		if _, err := pool.Select(SelectOptions{
			Site: "global", Model: "m", PreferQuota: true, RequestID: "r-pq",
		}); err != nil {
			t.Fatal(err)
		}
		e := obs.selection[0]
		if e.Source != SelSourcePreferQuota {
			t.Fatalf("Source 应为 %q，实际 %q", SelSourcePreferQuota, e.Source)
		}
	})

	t.Run("顺序轮转", func(t *testing.T) {
		pool := newObserverPool(t)
		acc := CreateAccount(Account{Label: "a", BearerToken: "t", Site: "global"})
		if _, _, err := pool.Upsert(acc); err != nil {
			t.Fatal(err)
		}
		obs := &fakeObserver{}
		pool.SetObserver(obs)

		if _, err := pool.Select(SelectOptions{Site: "global", Model: "m", RequestID: "r-rot"}); err != nil {
			t.Fatal(err)
		}
		e := obs.selection[0]
		if e.Source != SelSourceRotation {
			t.Fatalf("Source 应为 %q，实际 %q", SelSourceRotation, e.Source)
		}
	})
}
