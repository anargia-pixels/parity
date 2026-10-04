package parity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

var errLocked = errors.New("lock is already held")

type processLock struct {
	file *os.File
}

// acquireFileLock leaves the file in place so all callers lock the same inode.
func acquireFileLock(ctx context.Context, path string, wait bool) (*processLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			file.Close()
			return nil, err
		}
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &processLock{file: file}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			file.Close()
			return nil, err
		}
		if !wait {
			file.Close()
			return nil, errLocked
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func (lock *processLock) release() {
	syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
	lock.file.Close()
}

func acquireSyncLock(ctx context.Context, stateDir, label string) (*processLock, error) {
	lock, err := acquireFileLock(ctx, filepath.Join(stateDir, "locks", "entries", label+".lock"), false)
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(struct {
		PID int   `json:"pid"`
		At  int64 `json:"at"`
	}{os.Getpid(), time.Now().UnixMilli()})
	if err := lock.file.Truncate(0); err != nil {
		lock.release()
		return nil, fmt.Errorf("write sync lock: %w", err)
	}
	if _, err := lock.file.WriteAt(data, 0); err != nil {
		lock.release()
		return nil, fmt.Errorf("write sync lock: %w", err)
	}
	return lock, nil
}

func isPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
