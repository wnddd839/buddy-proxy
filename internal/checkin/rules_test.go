package checkin_test

import (
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/checkin"
)

func TestEmptyWindowMeansAllDay(t *testing.T) {
	store := checkin.Store{Rules: []checkin.Rule{
		{AccountID: "a1", Enabled: true, Start: "", End: ""},
	}}
	// Write 会跑 normalizeRules 校验；不报错即通过
	if err := checkin.Open(t.TempDir() + "/r.json").Write(store); err != nil {
		t.Fatalf("空窗口应视为全天，不应报错: %v", err)
	}
}

func TestEmptyStartWithEndIsRejected(t *testing.T) {
	store := checkin.Store{Rules: []checkin.Rule{
		{AccountID: "a1", Enabled: true, Start: "", End: "22:00"},
	}}
	err := checkin.Open(t.TempDir() + "/r.json").Write(store)
	if err == nil {
		t.Fatal("开始时间为空但结束时间有值，应报错")
	}
	// 现在会先归为 allDay 模式再校验，错误文案明确指向「全天模式」。
	if !strings.Contains(err.Error(), "全天模式") {
		t.Fatalf("错误信息应提到全天模式，got: %v", err)
	}
}

func TestFixedTimeStillWorks(t *testing.T) {
	store := checkin.Store{Rules: []checkin.Rule{
		{AccountID: "a1", Enabled: true, Start: "09:00", End: ""},
	}}
	if err := checkin.Open(t.TempDir() + "/r.json").Write(store); err != nil {
		t.Fatalf("固定时刻应仍然有效: %v", err)
	}
}

func TestWindowRangeStillWorks(t *testing.T) {
	store := checkin.Store{Rules: []checkin.Rule{
		{AccountID: "a1", Enabled: true, Start: "08:00", End: "10:30"},
	}}
	if err := checkin.Open(t.TempDir() + "/r.json").Write(store); err != nil {
		t.Fatalf("时间窗口应仍然有效: %v", err)
	}
}

func TestAllDayWindowIsPersisted(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/r.json"
	f := checkin.Open(path)
	if err := f.Write(checkin.Store{Rules: []checkin.Rule{
		{AccountID: "a1", Enabled: true, Start: "", End: ""},
	}}); err != nil {
		t.Fatal(err)
	}
	store, err := f.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Rules) != 1 {
		t.Fatalf("want 1 rule, got %d", len(store.Rules))
	}
	// 原文必须原样保留（空 Start / 空 End），不要被规范化成 00:00/23:59，
	// 否则前端回显会显示成具体时刻而丢失「全天」语义。
	if store.Rules[0].Start != "" || store.Rules[0].End != "" {
		t.Fatalf("应原样保留空窗口，got start=%q end=%q", store.Rules[0].Start, store.Rules[0].End)
	}
}
