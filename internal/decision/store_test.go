package decision_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

func TestAppendAndReadRange(t *testing.T) {
	dir := t.TempDir()
	st, err := decision.OpenStore(dir, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	credit := 1.3
	if err := st.Append(decision.Record{
		Kind: decision.KindQuota, At: 1000, AccountID: "a1",
		Op: decision.OpDeduct, After: &credit, Credit: &credit,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := st.ReadRange(time.UnixMilli(0), time.UnixMilli(2000))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].AccountID != "a1" || got[0].Op != "deduct" {
		t.Fatalf("got %+v", got)
	}
	if got[0].After == nil || *got[0].After != 1.3 {
		t.Fatalf("After 未保留：%+v", got[0].After)
	}
}

func TestAppendFillsAt(t *testing.T) {
	dir := t.TempDir()
	st, err := decision.OpenStore(dir, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().UnixMilli()
	if err := st.Append(decision.Record{Kind: decision.KindRequest, RequestID: "r1"}); err != nil {
		t.Fatal(err)
	}
	got, err := st.ReadRange(time.UnixMilli(before-1000), time.UnixMilli(before+5000))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].At < before {
		t.Fatalf("At 未填充：%+v", got)
	}
}

// 坏行（半行 / 手工编辑）不得让整段读不出来，也不能让其它行丢失。
func TestReadRangeSkipsBrokenLines(t *testing.T) {
	dir := t.TempDir()
	st, err := decision.OpenStore(dir, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UnixMilli()
	if err := st.Append(decision.Record{Kind: decision.KindRequest, At: at, RequestID: "r1"}); err != nil {
		t.Fatal(err)
	}

	// 直接往当天的文件里插一行坏 JSON（模拟写一半被杀）。
	date := time.UnixMilli(at).In(time.Local).Format("20060102")
	f, err := os.OpenFile(filepath.Join(dir, "proxy-decisions-"+date+".jsonl"),
		os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{not json\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if err := st.Append(decision.Record{Kind: decision.KindRequest, At: at + 1, RequestID: "r2"}); err != nil {
		t.Fatal(err)
	}
	got, err := st.ReadRange(time.UnixMilli(at-10), time.UnixMilli(at+10))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("坏行应被跳过、好行保留，实际读到 %d 条：%+v", len(got), got)
	}
}

// 区间外的记录不得混进来。
func TestReadRangeExcludesOutside(t *testing.T) {
	dir := t.TempDir()
	st, err := decision.OpenStore(dir, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	_ = st.Append(decision.Record{Kind: decision.KindRequest, At: now - 48*3600*1000, RequestID: "old"})
	_ = st.Append(decision.Record{Kind: decision.KindRequest, At: now, RequestID: "new"})

	got, err := st.ReadRange(time.UnixMilli(now-3600*1000), time.UnixMilli(now+1000))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].RequestID != "new" {
		t.Fatalf("区间过滤失败：%+v", got)
	}
}
