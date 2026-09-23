package atomicwrite

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFileWritesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")

	if err := Write(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first" {
		t.Fatalf("content=%q", got)
	}

	if err := Write(path, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Fatalf("content=%q", got)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected no temp leftovers, got %v", names)
	}
}

func TestFileSyncsBeforeRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ordered.json")

	var order []string
	orig := syncFile
	syncFile = func(f *os.File) error {
		// At sync time the temp file must exist and the target must not yet
		// carry the new content.
		if _, err := os.Stat(path + ".tmp"); err != nil {
			t.Fatalf("temp missing at sync time: %v", err)
		}
		if cur, err := os.ReadFile(path); err == nil && string(cur) == "new" {
			t.Fatal("target updated before sync")
		}
		order = append(order, "sync")
		return orig(f)
	}
	t.Cleanup(func() { syncFile = orig })

	if err := Write(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(order) != 1 {
		t.Fatalf("expected exactly one sync, got %v", order)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Fatalf("content=%q", got)
	}
}

func TestFileSyncErrorPreservesOldFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keep.json")
	if err := Write(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	orig := syncFile
	syncFile = func(f *os.File) error { return errors.New("sync failed") }
	t.Cleanup(func() { syncFile = orig })

	if err := Write(path, []byte("new"), 0o600); err == nil {
		t.Fatal("expected error")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Fatalf("old content must survive a failed write, got %q", got)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp must be cleaned up, stat err=%v", err)
	}
}

func TestFileEmptyPath(t *testing.T) {
	if err := Write("", []byte("x"), 0o600); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestFilePerm(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not carry POSIX permission bits")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "perm.json")
	if err := Write(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("perm=%v want no group/other bits", info.Mode().Perm())
	}
}
