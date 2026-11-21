package invariant

import (
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

func appendRec(t *testing.T, st *decision.Store, rec decision.Record) {
	t.Helper()
	if err := st.Append(rec); err != nil {
		t.Fatal(err)
	}
}

// 同一断言同一对象聚成一组；不同对象各一组。
func TestReplayGroupsByInvariantAndSubject(t *testing.T) {
	st, err := decision.OpenStore(t.TempDir(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	// 两条 I3（同 RequestID 不同时间仍在同一组？Subject 是 RequestID，故两条同 ID 才归组）
	appendRec(t, st, decision.Record{Kind: decision.KindRequest, At: now - 2000, RequestID: "same", InMessages: 5, OutMessages: 2})
	appendRec(t, st, decision.Record{Kind: decision.KindRequest, At: now - 1000, RequestID: "same", InMessages: 5, OutMessages: 2})
	// 一条 I4（不同对象）
	appendRec(t, st, decision.Record{Kind: decision.KindRequest, At: now, RequestID: "other", InSystemLen: 100, OutSystemLen: 0})

	rep, err := Replay(st, time.UnixMilli(now-10000), time.UnixMilli(now+1000), nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Scanned != 3 {
		t.Fatalf("扫描条数应为 3，实际 %d", rep.Scanned)
	}
	var i3, i4 *ReplayGroup
	for i := range rep.Groups {
		switch rep.Groups[i].Invariant {
		case "I3":
			i3 = &rep.Groups[i]
		case "I4":
			i4 = &rep.Groups[i]
		}
	}
	if i3 == nil || i3.Count != 2 {
		t.Fatalf("I3 应聚成 1 组共 2 次：%+v", rep.Groups)
	}
	if i3.FirstAt != now-2000 || i3.LastAt != now-1000 {
		t.Fatalf("首末时间不对：%+v", i3)
	}
	if i4 == nil || i4.Count != 1 {
		t.Fatalf("I4 应有一组：%+v", rep.Groups)
	}
	if rep.Coverage == "" {
		t.Fatal("必须打印覆盖说明")
	}
}

// only 过滤只保留指定断言。
func TestReplayOnlyFilters(t *testing.T) {
	st, err := decision.OpenStore(t.TempDir(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	appendRec(t, st, decision.Record{Kind: decision.KindRequest, At: now, RequestID: "a", InMessages: 5, OutMessages: 2})
	appendRec(t, st, decision.Record{Kind: decision.KindRequest, At: now, RequestID: "b", InSystemLen: 100, OutSystemLen: 0})

	rep, err := Replay(st, time.UnixMilli(now-1000), time.UnixMilli(now+1000), []string{"I4"})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range rep.Groups {
		if g.Invariant != "I4" {
			t.Fatalf("应只剩 I4：%+v", rep.Groups)
		}
	}
	if len(rep.Groups) != 1 {
		t.Fatalf("应有 1 组，实际 %d", len(rep.Groups))
	}
}

// 落盘的 finding 记录不能被当成输入再喂一遍（否则会自己命中自己）。
func TestReplaySkipsStoredFindings(t *testing.T) {
	st, err := decision.OpenStore(t.TempDir(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	f := decision.Finding{Invariant: "I3", Subject: "x", At: now, Detail: "old"}
	appendRec(t, st, decision.Record{Kind: decision.KindFinding, At: now, Finding: &f})

	rep, err := Replay(st, time.UnixMilli(now-1000), time.UnixMilli(now+1000), nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Scanned != 0 {
		t.Fatalf("finding 记录不该计入扫描：%d", rep.Scanned)
	}
	if len(rep.Groups) != 0 {
		t.Fatalf("不该有命中：%+v", rep.Groups)
	}
}

// 空目录必须给出空报告而不是 panic。
func TestReplayEmptyStore(t *testing.T) {
	rep, err := Replay(nil, time.Now().Add(-time.Hour), time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Coverage == "" {
		t.Fatal("空目录也要说明覆盖范围")
	}
}

// 跨天状态连续：昨天 47 万、今天 31 万仍判为骤降（state 不因跨文件重置）。
func TestReplayKeepsStateAcrossDays(t *testing.T) {
	st, err := decision.OpenStore(t.TempDir(), 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	day1 := time.Now().Add(-25 * time.Hour)
	day2 := time.Now().Add(-1 * time.Hour)
	appendRec(t, st, decision.Record{
		Kind: decision.KindRequest, At: day1.UnixMilli(), SessionKey: "s", AccountID: "a", Model: "m",
		PromptTokens: 477680, Ok: true, UsageSource: decision.UsageActual,
	})
	appendRec(t, st, decision.Record{
		Kind: decision.KindRequest, At: day2.UnixMilli(), SessionKey: "s", AccountID: "a", Model: "m",
		PromptTokens: 317173, Ok: true, UsageSource: decision.UsageActual,
	})

	rep, err := Replay(st, day1.Add(-time.Minute), day2.Add(time.Minute), []string{"I3b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Groups) != 1 || rep.Groups[0].Invariant != "I3b" {
		t.Fatalf("跨天也应识别：%+v", rep.Groups)
	}
}
