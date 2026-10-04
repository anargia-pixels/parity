package parity

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestForegroundWatcher(t *testing.T) {
	repos := newTestRepos(t)
	stateDir := t.TempDir()
	t.Setenv("PARITY_STATE_DIR", stateDir)
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	writeTestFile(t, filepath.Join(repos.local, "initial.txt"), "boot")
	watchPath := filepath.Join(repos.base, "linked-config")
	if err := os.Symlink(repos.local, watchPath); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "parity.toml")
	writeTestFile(t, configPath, "[watch]\nlocal_dir = '"+watchPath+"'\n")
	logPath := filepath.Join(t.TempDir(), "watch.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	cmd := exec.Command(testBinary, "watch", "--foreground", "--config", configPath)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			cmd.Process.Kill()
			<-done
		}
	})
	waitFor(t, 5*time.Second, func() bool {
		text, _ := os.ReadFile(logPath)
		return strings.Contains(string(text), "[watch] watching")
	})
	if pid := daemonPID(stateDir); pid != cmd.Process.Pid {
		t.Fatalf("watcher pid = %d, want %d", pid, cmd.Process.Pid)
	}
	if got := testGit(t, repos.bare, "show", "HEAD:initial.txt"); got != "boot" {
		t.Fatalf("initial sync: %q", got)
	}
	lock, err := acquireSyncLock(context.Background(), stateDir, "watch")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if lock != nil {
			lock.release()
		}
	})
	writeTestFile(t, filepath.Join(repos.local, "nested/changed.txt"), "first edit")
	time.Sleep(100 * time.Millisecond)
	writeTestFile(t, filepath.Join(repos.local, "nested/changed.txt"), "final edit")
	waitFor(t, 6*time.Second, func() bool {
		text, _ := os.ReadFile(logPath)
		return strings.Contains(string(text), "waiting for current sync")
	})
	state, err := readState(stateDir)
	if err != nil || state.Entries["watch"].Result != "ok" {
		t.Fatalf("waiting for a manual sync must not overwrite the previous result: %#v, %v", state, err)
	}
	lock.release()
	lock = nil
	waitFor(t, 10*time.Second, func() bool {
		result := git(context.Background(), repos.bare, "show", "HEAD:nested/changed.txt")
		return result.code == 0 && strings.TrimSpace(result.stdout) == "final edit"
	})
	// The .gitignore file is config data, rather than Git's internal directory.
	writeTestFile(t, filepath.Join(repos.local, ".gitignore"), "# selected config files\n")
	waitFor(t, 10*time.Second, func() bool {
		result := git(context.Background(), repos.bare, "show", "HEAD:.gitignore")
		return result.code == 0 && strings.Contains(result.stdout, "selected config files")
	})
	waitFor(t, 3*time.Second, func() bool {
		state, err := readState(stateDir)
		return err == nil && state.Entries["watch"].Result == "ok"
	})
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		stopped = true
		if err != nil {
			t.Fatalf("unclean watcher exit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not stop")
	}
	if _, err := os.Stat(filepath.Join(stateDir, "parity.pid")); !os.IsNotExist(err) {
		t.Fatalf("pid file was not removed: %v", err)
	}
}

func TestDaemonCommands(t *testing.T) {
	repos := newTestRepos(t)
	stateDir := t.TempDir()
	t.Setenv("PARITY_STATE_DIR", stateDir)
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	configPath := filepath.Join(t.TempDir(), "parity.toml")
	writeTestFile(t, configPath, "[watch]\nlocal_dir = '"+repos.local+"'\n")
	t.Cleanup(func() {
		if pid := daemonPID(stateDir); pid != 0 {
			syscall.Kill(pid, syscall.SIGTERM)
			waitFor(t, 5*time.Second, func() bool { return daemonPID(stateDir) == 0 })
		}
	})
	code, stdout, stderr := runTestCLI(t, "watch", "--config", configPath)
	if code != 0 || !strings.Contains(stdout, "started in the background (pid ") || strings.Contains(stdout, "pid -") {
		t.Fatalf("daemon startup: exit=%d, stdout=%s, stderr=%s", code, stdout, stderr)
	}
	waitFor(t, 5*time.Second, func() bool {
		text, _ := os.ReadFile(filepath.Join(stateDir, "parity.log"))
		return strings.Contains(string(text), "[watch] watching")
	})
	code, stdout, stderr = runTestCLI(t, "status")
	if code != 0 || !strings.Contains(stdout, "running (pid ") || !strings.Contains(stdout, "watch") {
		t.Fatalf("daemon status: exit=%d, stdout=%s, stderr=%s", code, stdout, stderr)
	}
	code, stdout, stderr = runTestCLI(t, "watch", "--foreground", "--config", configPath)
	if code != 1 || !strings.Contains(stderr, "already running") {
		t.Fatalf("duplicate watcher: exit=%d, stdout=%s, stderr=%s", code, stdout, stderr)
	}
	code, stdout, stderr = runTestCLI(t, "stop")
	if code != 0 || !strings.Contains(stdout, "stopped") || daemonPID(stateDir) != 0 {
		t.Fatalf("daemon stop: exit=%d, stdout=%s, stderr=%s", code, stdout, stderr)
	}
}

func TestGitPaths(t *testing.T) {
	if !isGitPath("/repo/.git/index") || isGitPath("/repo/.gitignore") || isGitPath("/repo/my.git.conf") {
		t.Fatal("only .git directory components should be ignored")
	}
	if !pathWithin("/repo/config", "/repo/config/sub/file") || pathWithin("/repo/config", "/repo/config-other/file") {
		t.Fatal("watch roots must match complete path components")
	}
}

func runTestCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, testBinary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("CLI timed out: %v: %s", args, stderr.String())
	}
	code := 0
	if err != nil {
		if cmd.ProcessState == nil {
			t.Fatal(err)
		}
		code = cmd.ProcessState.ExitCode()
	}
	return code, stdout.String(), stderr.String()
}

func TestWatchErrorsUseJournalPriority(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "journal")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	logWatchError(file, "first\nsecond")
	stat, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	sys := stat.Sys().(*syscall.Stat_t)
	t.Setenv("JOURNAL_STREAM", fmt.Sprintf("%d:%d", sys.Dev, sys.Ino))
	logWatchError(file, "third\nfourth")
	text, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(text)), "\n")
	if len(lines) != 4 || strings.HasPrefix(lines[0], "<3>") || strings.HasPrefix(lines[1], "<3>") || !strings.HasPrefix(lines[2], "<3>") || !strings.HasSuffix(lines[3], " fourth") || !strings.HasPrefix(lines[3], "<3>") {
		t.Fatalf("journal lines: %q", lines)
	}
}

func TestWatchLogLocation(t *testing.T) {
	stateDir := t.TempDir()
	if got := watchLogLocation(stateDir, &bytes.Buffer{}); got != filepath.Join(stateDir, "parity.log") {
		t.Fatalf("daemon log location: %q", got)
	}
	file, err := os.CreateTemp(stateDir, "journal")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	sys := stat.Sys().(*syscall.Stat_t)
	t.Setenv("JOURNAL_STREAM", fmt.Sprintf("%d:%d", sys.Dev, sys.Ino))
	if got := watchLogLocation(stateDir, file); got != "journalctl --user -u parity" {
		t.Fatalf("journal log location: %q", got)
	}
}
