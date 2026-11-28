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

// 出站后失败（上游 5xx）仍然要判 —— 这是撤销 !rec.Ok 的理由。
func TestI3CatchesPostOutboundFailureShrink(t *testing.T) {
	rec := decision.Record{
		Kind: decision.KindRequest, At: 1, Ok: false, OutObserved: true,
		RequestID: "r-5xx", InMessages: 5, OutMessages: 2, ErrorCode: "500",
	}
	got := DefaultRegistry().Run(rec, NewState(8, 8))
	hit := false
	for _, f := range got {
		if f.Invariant == "I3" {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("出站后失败的丢历史仍应命中 I3：%+v", got)
	}
}

// 旧记录兜底：没有 OutObserved 但 OutMessages>0，仍应判。
func TestI3LegacyRecordWithoutOutObserved(t *testing.T) {
	rec := decision.Record{
		Kind: decision.KindRequest, At: 1, Ok: true,
		RequestID: "r-old", InMessages: 5, OutMessages: 3,
	}
	got := DefaultRegistry().Run(rec, NewState(8, 8))
	hit := false
	for _, f := range got {
		if f.Invariant == "I3" {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("旧记录应照常判：%+v", got)
	}
}

// I4 的出站前失败闸门。
func TestI4SkipsPreOutboundFailure(t *testing.T) {
	rec := decision.Record{
		Kind: decision.KindRequest, At: 1, Ok: false,
		RequestID: "r-pre", InMessages: 2, InSystemLen: 1200,
		OutSystemLen: 0, SystemForm: "keep",
	}
	if got := DefaultRegistry().Run(rec, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("出站前失败不该命中 I4：%+v", got)
	}
}

// 对照：出站后失败的 system 消失仍要报。
func TestI4CatchesPostOutboundSystemVanish(t *testing.T) {
	rec := decision.Record{
		Kind: decision.KindRequest, At: 1, Ok: false, OutObserved: true,
		RequestID: "r-post", InMessages: 2, OutMessages: 2,
		InSystemLen: 1200, OutSystemLen: 0, SystemForm: "keep",
	}
	got := DefaultRegistry().Run(rec, NewState(8, 8))
	hit := false
	for _, f := range got {
		if f.Invariant == "I4" {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("出站后失败的系统消失应命中 I4：%+v", got)
	}
}
