package usagejournal_test

import (
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/usagejournal"
)

func TestJournalRecordAndView(t *testing.T) {
	j := usagejournal.New(10)
	now := time.Now()
	credit := 0.05
	j.Record(usagejournal.Entry{
		At:               now.UnixMilli(),
		ProxyRequestID:   "proxy1",
		Model:            "auto",
		Ok:               true,
		PromptTokens:     100,
		CompletionTokens: 20,
		CachedTokens:     40,
		Credit:           &credit,
		DurationMs:       1200,
	})

	view := j.View("day", 5, 0)
	if view.Summary.Requests != 1 {
		t.Fatalf("requests=%d", view.Summary.Requests)
	}
	if view.Summary.Credits != 0.05 {
		t.Fatalf("credits=%v", view.Summary.Credits)
	}
	if view.RequestsTotal != 1 || len(view.Requests) != 1 || view.Requests[0].ProxyRequestID != "proxy1" {
		t.Fatalf("requests=%+v total=%d", view.Requests, view.RequestsTotal)
	}
}

func TestJournalPagination(t *testing.T) {
	j := usagejournal.New(20)
	now := time.Now().UnixMilli()
	for i := 0; i < 25; i++ {
		j.Record(usagejournal.Entry{At: now, ProxyRequestID: "id" + string(rune('a'+i)), Model: "m"})
	}
	all := j.View("week", 20, 0)
	if all.RequestsTotal != 20 || len(all.Requests) != 20 {
		t.Fatalf("ring cap total=%d len=%d", all.RequestsTotal, len(all.Requests))
	}
	page2 := j.View("week", 20, 20)
	if page2.RequestsTotal != 20 || len(page2.Requests) != 0 {
		t.Fatalf("page2 total=%d len=%d", page2.RequestsTotal, len(page2.Requests))
	}
}

func TestJournalRingCapacity(t *testing.T) {
	j := usagejournal.New(3)
	now := time.Now().UnixMilli()
	for i := 0; i < 5; i++ {
		j.Record(usagejournal.Entry{At: now, ProxyRequestID: "id" + string(rune('a'+i)), Model: "m"})
	}
	view := j.View("week", 10, 0)
	if len(view.Requests) != 3 {
		t.Fatalf("len=%d want 3", len(view.Requests))
	}
	if view.Requests[0].ProxyRequestID != "ide" {
		t.Fatalf("newest=%s", view.Requests[0].ProxyRequestID)
	}
}

func TestMonthViewUsesWeekWindowWhenHistoryShort(t *testing.T) {
	j := usagejournal.New(10)
	now := time.Now()
	j.Record(usagejournal.Entry{
		At:               now.UnixMilli(),
		ProxyRequestID:   "recent",
		PromptTokens:     10,
		CompletionTokens: 1,
	})
	week := j.View("week", 10, 0)
	month := j.View("month", 10, 0)
	if len(week.Series) != len(month.Series) {
		t.Fatalf("month series=%d week=%d", len(month.Series), len(week.Series))
	}
	if month.Summary.TotalTokens != week.Summary.TotalTokens {
		t.Fatalf("month tokens=%d week=%d", month.Summary.TotalTokens, week.Summary.TotalTokens)
	}
}

func TestViewByModelAndFilters(t *testing.T) {
	j := usagejournal.New(20)
	now := time.Now().UnixMilli()
	j.Record(usagejournal.Entry{
		At: now, ProxyRequestID: "hy3-1", Model: "hy3", AccountLabel: "alice",
		PromptTokens: 1000, CompletionTokens: 10, CachedTokens: 0,
	})
	j.Record(usagejournal.Entry{
		At: now, ProxyRequestID: "ds-1", Model: "deepseek-v4.1-flash", AccountLabel: "alice",
		PromptTokens: 1000, CompletionTokens: 10, CachedTokens: 800,
	})
	j.Record(usagejournal.Entry{
		At: now, ProxyRequestID: "ds-2", Model: "deepseek-v4.1-flash", AccountLabel: "bob",
		PromptTokens: 500, CompletionTokens: 5, CachedTokens: 400,
	})

	view := j.Query(usagejournal.Query{Range: "day", Limit: 20})
	if len(view.ByModel) != 2 {
		t.Fatalf("byModel=%d %+v", len(view.ByModel), view.ByModel)
	}
	by := map[string]usagejournal.ModelStat{}
	for _, row := range view.ByModel {
		by[row.Model] = row
	}
	if by["hy3"].CacheHitRate != 0 {
		t.Fatalf("hy3 rate=%v", by["hy3"].CacheHitRate)
	}
	if by["deepseek-v4.1-flash"].CacheHitRate < 79 || by["deepseek-v4.1-flash"].CacheHitRate > 81 {
		t.Fatalf("ds rate=%v", by["deepseek-v4.1-flash"].CacheHitRate)
	}

	hy3 := j.Query(usagejournal.Query{Range: "day", Model: "hy3", Limit: 20})
	if hy3.RequestsTotal != 1 || hy3.Summary.CachedTokens != 0 {
		t.Fatalf("hy3 filter total=%d cached=%d", hy3.RequestsTotal, hy3.Summary.CachedTokens)
	}
	bob := j.Query(usagejournal.Query{Range: "day", Account: "bob", Limit: 20})
	if bob.RequestsTotal != 1 || bob.Requests[0].AccountLabel != "bob" {
		t.Fatalf("account filter %+v", bob.Requests)
	}
}
