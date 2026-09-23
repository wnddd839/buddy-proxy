// Package atomicwrite writes files via temp + rename with the data flushed
// to disk before the rename.
//
// temp+rename alone only protects against process crashes: on NTFS the rename
// metadata can reach the journal while the file contents are still in the OS
// write-back cache, so a power loss in that window leaves a file whose length
// is correct but whose contents are zeroes.
//
// The temp file is named "<base>.tmp" (a fixed, sibling name) so that an
// interrupted write leaves at most one stale temp per target, which the next
// successful write replaces; the file is synced before the rename, so a crash
// or power loss leaves either the old file or the complete new one.
package atomicwrite

import (
	"os"
)

// syncFile is indirected so tests can assert the sync happens before rename.
var syncFile = func(f *os.File) error { return f.Sync() }

// Write writes data to path atomically and durably: the temporary file is
// synced (FlushFileBuffers on Windows) before it is renamed over the target.
// The target's directory must exist.
func Write(path string, data []byte, perm os.FileMode) error {
	if path == "" {
		return os.ErrInvalid
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	cleanup := func() { _ = os.Remove(tmp) }

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		cleanup()
		return err
	}
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		cleanup()
		return err
	}
	if err := syncFile(f); err != nil {
		_ = f.Close()
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		cleanup()
		return err
	}
	return nil
}
