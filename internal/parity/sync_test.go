package parity

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestSyncCommitsAndPushes(t *testing.T) {
	repos := newTestRepos(t)
	writeTestFile(t, filepath.Join(repos.local, "a.txt"), "hello")
	outcome := syncEntry(context.Background(), entry{"happy", repos.local}, syncOptions{stateDir: t.TempDir()})
	if outcome.status != "ok" {
		t.Fatalf("sync: %#v", outcome)
	}
	if got := testGit(t, repos.bare, "show", "HEAD:a.txt"); got != "hello" {
		t.Fatalf("remote content: %q", got)
	}
	if log := testGit(t, repos.bare, "log", "-1", "--format=%s"); !strings.HasPrefix(log, "sync ") {
		t.Fatalf("commit message: %q", log)
	}
}

func TestSyncEmptyRepository(t *testing.T) {
	repos := newTestRepos(t)
	outcome := syncEntry(context.Background(), entry{"empty", repos.local}, syncOptions{stateDir: t.TempDir()})
	if outcome.status != "skipped" || outcome.reason != "empty" {
		t.Fatalf("empty sync: %#v", outcome)
	}
}

func TestSyncSecrets(t *testing.T) {
	repos := newTestRepos(t)
	options := syncOptions{stateDir: t.TempDir()}
	// Newlines and Git pathspec syntax must not bypass the secret scan.
	file := ":(exclude)credential\nfile.toml"
	writeTestFile(t, filepath.Join(repos.local, file), "api_key = 'abcdefghijklmnopqr'\n")
	blocked := syncEntry(context.Background(), entry{"secrets", repos.local}, options)
	if blocked.status != "skipped" || blocked.reason != "secrets" || !slices.Contains(blocked.files, file) {
		t.Fatalf("blocked sync: %#v", blocked)
	}
	if result := git(context.Background(), repos.bare, "rev-parse", "--verify", "HEAD"); result.code == 0 {
		t.Fatal("secret was committed to the remote")
	}
	options.allowSecrets = true
	if allowed := syncEntry(context.Background(), entry{"secrets", repos.local}, options); allowed.status != "ok" {
		t.Fatalf("allow-secrets sync: %#v", allowed)
	}
}

func TestSyncAllowlist(t *testing.T) {
	repos := newTestRepos(t)
	writeTestFile(t, filepath.Join(repos.local, ".gitignore"), "*\n!.gitignore\n!config.toml\n!themes/\n!themes/colors.toml\n")
	writeTestFile(t, filepath.Join(repos.local, "config.toml"), "theme = 'dark'\n")
	writeTestFile(t, filepath.Join(repos.local, "themes/colors.toml"), "background = 'black'\n")
	writeTestFile(t, filepath.Join(repos.local, "themes/credentials.json"), "private credentials")
	writeTestFile(t, filepath.Join(repos.local, ".env"), "api_key = 'abcdefghijklmnopqr'\n")
	options := syncOptions{stateDir: t.TempDir()}
	if outcome := syncEntry(context.Background(), entry{"allowlist", repos.local}, options); outcome.status != "ok" {
		t.Fatalf("allowlist sync: %#v", outcome)
	}
	files := testGit(t, repos.bare, "ls-tree", "-r", "--name-only", "HEAD")
	if files != ".gitignore\nconfig.toml\nthemes/colors.toml" {
		t.Fatalf("unexpected tracked files: %s", files)
	}
}

func TestSyncStashRecovery(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		name := "clean restore"
		if conflict {
			name = "conflict preserves stash"
		}
		t.Run(name, func(t *testing.T) {
			repos := newTestRepos(t)
			path := filepath.Join(repos.local, "f.txt")
			writeTestFile(t, path, "one\ntwo\nthree\nfour\nfive\n")
			testGit(t, repos.local, "add", ".")
			testGit(t, repos.local, "commit", "-m", "base")
			testGit(t, repos.local, "push")
			second := filepath.Join(repos.base, "second")
			testGit(t, repos.base, "clone", repos.bare, second)
			writeTestFile(t, filepath.Join(second, "f.txt"), "ONE\ntwo\nthree\nfour\nfive\n")
			remoteText := "one\ntwo\nthree\nfour\nFIVE\n"
			if conflict {
				remoteText = "remote\ntwo\nthree\nfour\nfive\n"
			}
			writeTestFile(t, path, remoteText)
			testGit(t, repos.local, "add", ".")
			testGit(t, repos.local, "commit", "-m", "remote change")
			testGit(t, repos.local, "push")
			outcome := syncEntry(context.Background(), entry{"stash", second}, syncOptions{stateDir: t.TempDir()})
			stash := testGit(t, second, "stash", "list")
			if conflict {
				if outcome.status != "error" || !strings.Contains(outcome.message, "stash preserved") || stash == "" {
					t.Fatalf("conflict: %#v, stash=%q", outcome, stash)
				}
				return
			}
			if outcome.status != "ok" || stash != "" {
				t.Fatalf("restore: %#v, stash=%q", outcome, stash)
			}
			if got := testGit(t, repos.bare, "show", "HEAD:f.txt"); got != "ONE\ntwo\nthree\nfour\nFIVE" {
				t.Fatalf("merged content: %q", got)
			}
		})
	}
}

func TestSyncRestoresEditsAfterFailedPull(t *testing.T) {
	repos := newTestRepos(t)
	path := filepath.Join(repos.local, "f.txt")
	writeTestFile(t, path, "base")
	testGit(t, repos.local, "add", ".")
	testGit(t, repos.local, "commit", "-m", "base")
	testGit(t, repos.local, "push")
	writeTestFile(t, path, "local edits")
	testGit(t, repos.local, "remote", "set-url", "origin", filepath.Join(repos.base, "missing.git"))
	outcome := syncEntry(context.Background(), entry{"pull-error", repos.local}, syncOptions{stateDir: t.TempDir()})
	text, _ := os.ReadFile(path)
	if outcome.status != "error" || !strings.Contains(outcome.message, "local changes restored") || string(text) != "local edits" {
		t.Fatalf("failed pull lost local edits: %#v, content=%q", outcome, text)
	}
}

func TestSyncRetriesFailedPush(t *testing.T) {
	repos := newTestRepos(t)
	hook := filepath.Join(repos.bare, "hooks/pre-receive")
	writeTestFile(t, hook, "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(hook, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repos.local, "a.txt"), "hello")
	options := syncOptions{stateDir: t.TempDir()}
	if outcome := syncEntry(context.Background(), entry{"retry", repos.local}, options); outcome.status != "error" || !strings.Contains(outcome.message, "git push failed") {
		t.Fatalf("first push: %#v", outcome)
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	if outcome := syncEntry(context.Background(), entry{"retry", repos.local}, options); outcome.status != "ok" {
		t.Fatalf("retry push: %#v", outcome)
	}
	if got := testGit(t, repos.bare, "show", "HEAD:a.txt"); got != "hello" {
		t.Fatalf("retry did not publish existing commit: %q", got)
	}
}

func TestSyncInvalidRepository(t *testing.T) {
	isolateGit(t)
	plain := t.TempDir()
	options := syncOptions{stateDir: t.TempDir()}
	for _, test := range []struct{ dir, message string }{
		{filepath.Join(plain, "missing"), "directory does not exist"},
		{plain, "not a git repository"},
	} {
		if outcome := syncEntry(context.Background(), entry{"plain", test.dir}, options); outcome.status != "error" || !strings.Contains(outcome.message, test.message) {
			t.Fatalf("invalid repo: %#v", outcome)
		}
	}
	testGit(t, plain, "init", "-b", "master")
	if outcome := syncEntry(context.Background(), entry{"plain", plain}, options); outcome.status != "error" || !strings.Contains(outcome.message, "no git remote") {
		t.Fatalf("missing remote: %#v", outcome)
	}
}

func TestSyncRespectsLock(t *testing.T) {
	repos := newTestRepos(t)
	stateDir := t.TempDir()
	lock, err := acquireSyncLock(context.Background(), stateDir, "locked")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.release()
	if outcome := syncEntry(context.Background(), entry{"locked", repos.local}, syncOptions{stateDir: stateDir}); outcome.reason != "locked" {
		t.Fatalf("overlapping sync: %#v", outcome)
	}
}

func TestSyncLabelDoesNotUseWatcherLock(t *testing.T) {
	repos := newTestRepos(t)
	stateDir := t.TempDir()
	lock, err := acquireFileLock(context.Background(), filepath.Join(stateDir, "locks", "watcher.lock"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.release()
	writeTestFile(t, filepath.Join(repos.local, "config.toml"), "theme = 'dark'")
	outcome := syncEntry(context.Background(), entry{"watcher", repos.local}, syncOptions{stateDir: stateDir})
	if outcome.status != "ok" {
		t.Fatalf("a valid label must not collide with the daemon's lock: %#v", outcome)
	}
}

func TestSyncReceivesRemoteChanges(t *testing.T) {
	repos := newTestRepos(t)
	path := filepath.Join(repos.local, "config.toml")
	writeTestFile(t, path, "theme = 'light'")
	testGit(t, repos.local, "add", ".")
	testGit(t, repos.local, "commit", "-m", "base")
	testGit(t, repos.local, "push")
	second := filepath.Join(repos.base, "second")
	testGit(t, repos.base, "clone", repos.bare, second)
	writeTestFile(t, path, "theme = 'dark'")
	testGit(t, repos.local, "add", ".")
	testGit(t, repos.local, "commit", "-m", "remote edit")
	testGit(t, repos.local, "push")
	outcome := syncEntry(context.Background(), entry{"incoming", second}, syncOptions{stateDir: t.TempDir()})
	if outcome.status != "ok" {
		t.Fatalf("receiving changes should report synced, not already up to date: %#v", outcome)
	}
	text, err := os.ReadFile(filepath.Join(second, "config.toml"))
	if err != nil || string(text) != "theme = 'dark'" {
		t.Fatalf("incoming changes: %q, %v", text, err)
	}
}

func TestSyncIdentityFallback(t *testing.T) {
	repos := newTestRepos(t)
	for _, name := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"} {
		previous, wasSet := os.LookupEnv(name)
		os.Unsetenv(name)
		t.Cleanup(func() {
			if wasSet {
				os.Setenv(name, previous)
			} else {
				os.Unsetenv(name)
			}
		})
	}
	writeTestFile(t, filepath.Join(repos.local, "config.toml"), "theme = 'dark'")
	if outcome := syncEntry(context.Background(), entry{"identity", repos.local}, syncOptions{stateDir: t.TempDir()}); outcome.status != "ok" {
		t.Fatalf("sync without Git identity: %#v", outcome)
	}
	if author := testGit(t, repos.bare, "log", "-1", "--format=%an <%ae>"); author != "parity <parity@localhost>" {
		t.Fatalf("fallback author: %q", author)
	}
	if result := git(context.Background(), repos.local, "config", "--get", "user.name"); result.code == 0 {
		t.Fatal("fallback identity must not modify Git configuration")
	}
}

func TestSyncCancellationReleasesLock(t *testing.T) {
	repos := newTestRepos(t)
	stateDir := t.TempDir()
	marker := filepath.Join(repos.base, "hook-started")
	hook := filepath.Join(repos.local, ".git/hooks/pre-commit")
	writeTestFile(t, hook, "#!/bin/sh\ntouch '"+marker+"'\nsleep 30\n")
	if err := os.Chmod(hook, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repos.local, "config.toml"), "theme = 'dark'")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan syncOutcome, 1)
	go func() { done <- syncEntry(ctx, entry{"cancel", repos.local}, syncOptions{stateDir: stateDir}) }()
	waitFor(t, 5*time.Second, func() bool { _, err := os.Stat(marker); return err == nil })
	cancel()
	select {
	case outcome := <-done:
		if outcome.status != "error" || !strings.Contains(outcome.message, "context canceled") {
			t.Fatalf("canceled sync: %#v", outcome)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not stop Git and its helpers")
	}
	lock, err := acquireSyncLock(context.Background(), stateDir, "cancel")
	if err != nil {
		t.Fatalf("canceled sync left its lock held: %v", err)
	}
	lock.release()
}
