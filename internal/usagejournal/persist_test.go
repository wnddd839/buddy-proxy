package usagejournal_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/usagejournal"
)

func TestAllRangeAfterOpeningLegacySnapshot(t *testing.T) {
	now := time.Now()
	expired := now.AddDate(0, 0, -100)
	retained := now.AddDate(0, 0, -40)
	path := filepath.Join(t.TempDir(), "proxy-usage.json")
	snapshot := fmt.Sprintf(`{"version":1,"entries":[
		{"at":%d,"proxyRequestId":"expired","model":"old"},
		{"at":%d,"proxyRequestId":"retained","model":"current"}
	],"byDay":{"%s":{"requests":1},"%s":{"requests":1}}}`,
		expired.UnixMilli(), retained.UnixMilli(),
		expired.Format(time.DateOnly), retained.Format(time.DateOnly))
	if err := os.WriteFile(path, []byte(snapshot), 0o600); err != nil {
		t.Fatal(err)
	}
	j, err := usagejournal.Open(path, 400)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := j.Close(); err != nil {
			t.Errorf("close journal: %v", err)
		}
	})

	view := j.View("all", 20, 0)
	if view.Summary.Requests != 1 || view.RequestsTotal != 1 ||
		len(view.Requests) != 1 || view.Requests[0].ProxyRequestID != "retained" {
		t.Fatalf("summary=%d, details=%d, rows=%+v", view.Summary.Requests, view.RequestsTotal, view.Requests)
	}
	if got := time.UnixMilli(view.Summary.From).Format(time.DateOnly); got != retained.Format(time.DateOnly) {
		t.Fatalf("chart starts on %s, want %s", got, retained.Format(time.DateOnly))
	}
	filtered := j.Query(usagejournal.Query{Range: "all", Model: "old"})
	if filtered.RequestsTotal != 0 || filtered.Summary.Requests != 0 {
		t.Fatalf("expired request returned by model filter: %+v", filtered.Requests)
	}
}

func TestOpenPersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy-usage.json")

	j1, err := usagejournal.Open(path, 50)
	if err != nil {
		t.Fatal(err)
	}
	credit := 0.1
	j1.Record(usagejournal.Entry{
		At:               time.Now().UnixMilli(),
		ProxyRequestID:   "persist-1",
		Model:            "auto",
		Ok:               true,
		PromptTokens:     100,
		CompletionTokens: 10,
		CachedTokens:     40,
		Credit:           &credit,
	})
	if err := j1.Close(); err != nil {
		t.Fatal(err)
	}

	j2, err := usagejournal.Open(path, 50)
	if err != nil {
		t.Fatal(err)
	}
	defer j2.Close()

	view := j2.View("day", 20, 0)
	if view.RequestsTotal != 1 {
		t.Fatalf("total=%d", view.RequestsTotal)
	}
	if view.Summary.TotalTokens != 110 {
		t.Fatalf("tokens=%d", view.Summary.TotalTokens)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
