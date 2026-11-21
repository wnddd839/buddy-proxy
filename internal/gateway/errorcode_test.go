package gateway

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
	"github.com/wnddd839/codebuddy-proxy/internal/invariant"
)

// 有数字码的错误必须被识别出来，不能一律算「未分类」。
func TestClassifyForDecision(t *testing.T) {
	cases := []struct {
		name     string
		ok       bool
		canceled bool
		errMsg   string
		provided string
		wantCode string
		wantKind string
	}{
		{"成功", true, false, "", "", "", ""},
		{"取消", false, true, "boom", "", "", "canceled"},
		{"已有码", false, false, "x", "14018", "14018", "coded"},
		{"括号码", false, false, "upstream said (code 11128) nope", "", "11128", "coded"},
		{"裸码", false, false, "error 11140 happened", "", "11140", "coded"},
		{"空原文", false, false, "   ", "", "", "empty"},
		{"真未分类", false, false, "something odd happened", "", "", "unclassified"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, kind := classifyForDecision(c.ok, c.canceled, c.errMsg, c.provided)
			if code != c.wantCode || kind != c.wantKind {
				t.Fatalf("got (%q,%q) want (%q,%q)", code, kind, c.wantCode, c.wantKind)
			}
		})
	}
}

// I5 只对「有原文但没有码」计数；取消与空回包不得进分母。
func TestI5IgnoresAlreadyClassified(t *testing.T) {
	reg := invariant.DefaultRegistry()
	st := invariant.NewState(8, 8)
	base := decision.Record{Kind: decision.KindRequest, At: 1, Ok: false, ErrorText: "boom"}

	for i := 0; i < 5; i++ {
		// 取消
		r := base
		r.Canceled = true
		r.ErrorKind = "canceled"
		if got := reg.Run(r, st); len(got) != 0 {
			t.Fatalf("取消不该进 I5：%+v", got)
		}
		// 有码
		r2 := base
		r2.ErrorCode = "11128"
		r2.ErrorKind = "coded"
		if got := reg.Run(r2, st); len(got) != 0 {
			t.Fatalf("有码不该进 I5：%+v", got)
		}
		// 空原文
		r3 := decision.Record{Kind: decision.KindRequest, At: 1, Ok: false, ErrorKind: "empty"}
		if got := reg.Run(r3, st); len(got) != 0 {
			t.Fatalf("空原文不该进 I5：%+v", got)
		}
	}

	// 真未分类：第 3 次报一次。
	r := base
	r.ErrorKind = "unclassified"
	for i := 1; i <= 2; i++ {
		if got := reg.Run(r, st); len(got) != 0 {
			t.Fatalf("第 %d 次不该报", i)
		}
	}
	got := reg.Run(r, st)
	if len(got) != 1 || got[0].Invariant != "I5" {
		t.Fatalf("第 3 次应报：%+v", got)
	}
}
