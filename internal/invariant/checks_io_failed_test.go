package invariant

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 失败请求不判 I3：出站 0 条是因为压根没发出去，不是历史被丢。
func TestI3SkipsFailedRequests(t *testing.T) {
	rec := decision.Record{
		Kind: decision.KindRequest, At: 1, Ok: false,
		RequestID: "r-failed", InMessages: 1, OutMessages: 0,
		ErrorText: "no valid messages after normalization",
	}
	if got := DefaultRegistry().Run(rec, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("失败请求不该命中 I3：%+v", got)
	}
}

// 对照：成功的请求仍然要判（否则修复就把真缺陷也放过了）。
func TestI3StillCatchesSuccessShrink(t *testing.T) {
	rec := decision.Record{
		Kind: decision.KindRequest, At: 1, Ok: true,
		RequestID: "r-ok", InMessages: 5, OutMessages: 3,
	}
	got := DefaultRegistry().Run(rec, NewState(8, 8))
	hit := false
	for _, f := range got {
		if f.Invariant == "I3" {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("成功请求丢历史仍应命中 I3：%+v", got)
	}
}

// 对照：字节归零判据对失败请求同样不判。
func TestI3SkipsFailedBytesZero(t *testing.T) {
	rec := decision.Record{
		Kind: decision.KindRequest, At: 1, Ok: false,
		RequestID: "r-failed2", InMessages: 2, OutMessages: 0,
		InBytes: 900, OutBytes: 0,
	}
	if got := DefaultRegistry().Run(rec, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("失败请求不该命中 I3：%+v", got)
	}
}
