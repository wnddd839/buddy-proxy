package usagejournal_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/usagejournal"
)

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
