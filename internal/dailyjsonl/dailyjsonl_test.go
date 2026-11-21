package dailyjsonl_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/dailyjsonl"
)

const prefix = "proxy-test-"

func TestPathAndDates(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"20261119", "20261120"} {
		if err := os.WriteFile(dailyjsonl.Path(dir, prefix, d), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// 同目录里另一个前缀的文件不得被算进来。
	if err := os.WriteFile(dailyjsonl.Path(dir, "proxy-other-", "20261118"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := dailyjsonl.Dates(dir, prefix)
	if len(got) != 2 || got[0] != "20261119" || got[1] != "20261120" {
		t.Fatalf("Dates = %v", got)
	}
	if base := filepath.Base(dailyjsonl.Path(dir, prefix, "20261120")); base != "proxy-test-20261120.jsonl" {
		t.Fatalf("Path = %s", base)
	}
}

func TestReadDateSkipsBlankLines(t *testing.T) {
	dir := t.TempDir()
	p := dailyjsonl.Path(dir, prefix, "20261120")
	if err := os.WriteFile(p, []byte("{}\n\n \n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := dailyjsonl.ReadDate(dir, prefix, "20261120")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines=%d", len(lines))
	}
}

func TestPruneRemovesOldFiles(t *testing.T) {
	dir := t.TempDir()
	old := dailyjsonl.Path(dir, prefix, "20260101")
	keep := dailyjsonl.Path(dir, prefix, "20261120")
	other := dailyjsonl.Path(dir, "proxy-other-", "20260101") // 别的家族，不在扫描范围内
	for _, p := range []string{old, keep, other} {
		if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := dailyjsonl.Prune(dir, prefix, 30*24*time.Hour, time.Date(2026, 11, 21, 0, 0, 0, 0, time.Local))
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("保留的文件被删了")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("别的家族被误删")
	}
}
