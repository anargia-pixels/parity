package parity

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestLocksExcludeOtherCallersAndRelease(t *testing.T) {
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, "locks", "entries", "editor.lock")
	writeTestFile(t, path, "corrupt stale lock")
	lock, err := acquireSyncLock(context.Background(), stateDir, "editor")
	if err != nil {
		t.Fatal(err)
	}
	second, err := acquireSyncLock(context.Background(), stateDir, "editor")
	if second != nil || !errors.Is(err, errLocked) {
		t.Fatalf("duplicate lock: %#v, %v", second, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := acquireFileLock(ctx, path, true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting lock must respect cancellation: %v", err)
	}
	lock.release()
	lock, err = acquireSyncLock(context.Background(), stateDir, "editor")
	if err != nil {
		t.Fatalf("released lock must be reusable: %v", err)
	}
	lock.release()
}
