package parity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounceDelay = 3 * time.Second

type watchedEntry struct {
	entry
	root     string
	deadline time.Time
	running  bool
}

type watchResult struct {
	label    string
	outcome  syncOutcome
	stateErr error
}

func runWatcher(ctx context.Context, cfg config, stateDir string, stdout, stderr io.Writer) error {
	lock, err := acquireFileLock(ctx, filepath.Join(stateDir, "locks", "watcher.lock"), false)
	if errors.Is(err, errLocked) {
		return fmt.Errorf("parity watch is already running (pid %d)", daemonPID(stateDir))
	}
	if err != nil {
		return err
	}
	defer lock.release()
	// The held lock makes any existing PID file stale.
	pidPath := filepath.Join(stateDir, "parity.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return err
	}
	defer os.Remove(pidPath)
	signalReady(nil)
	report := func(result entryOutcome) {
		reportWatchOutcome(result.label, result.outcome, stateDir, stdout, stderr)
	}
	logWatch(stdout, "initial sync")
	_, err = syncAll(ctx, cfg.entries, syncOptions{stateDir: stateDir}, report)
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return err
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()
	var entries []*watchedEntry
	for _, item := range cfg.entries {
		root, err := filepath.Abs(item.localDir)
		if err == nil {
			root, err = filepath.EvalSymlinks(root)
		}
		if err == nil {
			err = addWatchTree(watcher, root)
		}
		if err != nil {
			logWatchError(stderr, fmt.Sprintf("[%s] failed to watch %s: %s", item.label, item.localDir, err))
			continue
		}
		entries = append(entries, &watchedEntry{entry: item, root: root})
		logWatch(stdout, fmt.Sprintf("[%s] watching %s", item.label, item.localDir))
	}
	return watchChanges(ctx, watcher, entries, stateDir, stdout, stderr)
}

func watchChanges(ctx context.Context, watcher *fsnotify.Watcher, entries []*watchedEntry, stateDir string, stdout, stderr io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	results := make(chan watchResult, len(entries))
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
	}()
	for {
		timerChannel := resetWatchTimer(timer, entries)
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return fmt.Errorf("filesystem watcher stopped unexpectedly")
			}
			if isGitPath(event.Name) {
				continue
			}
			if err := addCreatedDirectory(watcher, event); err != nil {
				logWatchError(stderr, fmt.Sprintf("failed to watch %s: %s", event.Name, err))
			}
			for _, item := range entries {
				if pathWithin(item.root, event.Name) {
					item.deadline = time.Now().Add(debounceDelay)
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return fmt.Errorf("filesystem watcher stopped unexpectedly")
			}
			logWatchError(stderr, "watch error: "+err.Error())
		case result := <-results:
			for _, item := range entries {
				if item.label != result.label {
					continue
				}
				item.running = false
				if result.outcome.reason == reasonLocked {
					item.deadline = time.Now().Add(debounceDelay)
				}
			}
			if result.outcome.reason == reasonLocked {
				logWatch(stdout, fmt.Sprintf("[%s] waiting for current sync", result.label))
				continue
			}
			reportWatchOutcome(result.label, result.outcome, stateDir, stdout, stderr)
			if result.stateErr != nil {
				logWatchError(stderr, "could not save sync status: "+result.stateErr.Error())
			}
		case <-timerChannel:
			for _, item := range entries {
				if item.running || item.deadline.IsZero() || time.Now().Before(item.deadline) {
					continue
				}
				item.running = true
				item.deadline = time.Time{}
				logWatch(stdout, fmt.Sprintf("[%s] syncing", item.label))
				workers.Add(1)
				go func(item entry) {
					defer workers.Done()
					outcome := syncEntry(ctx, item, syncOptions{stateDir: stateDir})
					var err error
					if outcome.reason != reasonLocked {
						err = writeState(ctx, stateDir, map[string]entryState{item.label: stateUpdate(outcome)})
					}
					select {
					case results <- watchResult{item.label, outcome, err}:
					case <-ctx.Done():
					}
				}(item.entry)
			}
		}
	}
}

func resetWatchTimer(timer *time.Timer, entries []*watchedEntry) <-chan time.Time {
	var next time.Time
	for _, item := range entries {
		if !item.running && !item.deadline.IsZero() && (next.IsZero() || item.deadline.Before(next)) {
			next = item.deadline
		}
	}
	timer.Stop()
	if next.IsZero() {
		return nil
	}
	delay := time.Until(next)
	if delay < 0 {
		delay = 0
	}
	timer.Reset(delay)
	return timer.C
}

func addWatchTree(watcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, file fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !file.IsDir() {
			return nil
		}
		if file.Name() == ".git" {
			return filepath.SkipDir
		}
		return watcher.Add(path)
	})
}

func addCreatedDirectory(watcher *fsnotify.Watcher, event fsnotify.Event) error {
	if !event.Has(fsnotify.Create) {
		return nil
	}
	stat, err := os.Stat(event.Name)
	if err != nil || !stat.IsDir() {
		return nil
	}
	return addWatchTree(watcher, event.Name)
}

func isGitPath(path string) bool {
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if part == ".git" {
			return true
		}
	}
	return false
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func logWatch(writer io.Writer, message string) {
	writeLog(writer, "", message)
}

// logWatchError gives each line the error priority when stderr is the
// systemd journal, so journalctl highlights it and -p err selects it.
func logWatchError(writer io.Writer, message string) {
	priority := ""
	if isJournal(writer) {
		priority = "<3>"
	}
	writeLog(writer, priority, message)
}

// writeLog timestamps every line because the journal stores each one separately.
func writeLog(writer io.Writer, priority, message string) {
	stamp := timestamp()
	for _, line := range strings.Split(strings.TrimRight(message, "\n"), "\n") {
		fmt.Fprintf(writer, "%s%s %s\n", priority, stamp, line)
	}
}

// isJournal reports whether writer is the stream systemd connected to the
// journal. JOURNAL_STREAM can leak into child shells, so match the file too.
func isJournal(writer io.Writer) bool {
	stream := os.Getenv("JOURNAL_STREAM")
	file, ok := writer.(*os.File)
	if stream == "" || !ok {
		return false
	}
	stat, err := file.Stat()
	if err != nil {
		return false
	}
	sys, ok := stat.Sys().(*syscall.Stat_t)
	return ok && stream == fmt.Sprintf("%d:%d", sys.Dev, sys.Ino)
}

// watchLogLocation tells desktop notifications where the full error is.
func watchLogLocation(stateDir string, stderr io.Writer) string {
	switch {
	case isJournal(stderr):
		return "journalctl --user -u parity"
	case isTerminal(stderr):
		return "the parity watch output"
	default:
		return filepath.Join(stateDir, "parity.log")
	}
}

func reportWatchOutcome(label string, outcome syncOutcome, stateDir string, stdout, stderr io.Writer) {
	switch outcome.status {
	case statusOK:
		logWatch(stdout, fmt.Sprintf("[%s] synced", label))
	case statusSkipped:
		if outcome.reason == reasonSecrets {
			message := "possible secrets in " + strings.Join(outcome.files, ", ") + " — commit and push skipped"
			logWatchError(stderr, fmt.Sprintf("[%s] %s", label, message))
			notify("parity: "+label, message)
		} else {
			logWatch(stdout, fmt.Sprintf("[%s] nothing to commit", label))
		}
	default:
		logWatchError(stderr, fmt.Sprintf("[%s] error: %s", label, outcome.message))
		if outcome.reason == reasonAuth {
			logWatchError(stderr, credentialsHelp)
			notify("parity: "+label+" needs Git credentials", "See "+watchLogLocation(stateDir, stderr))
			return
		}
		notify("parity: "+label+" sync failed", outcome.message)
	}
}
