package parity

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	for _, argv := range [][]string{
		{"--config", "custom.toml", "sync", "editor", "--allow-secrets"},
		{"sync", "editor", "--allow-secrets", "--config=custom.toml"},
	} {
		args, err := parseArgs(argv)
		if err != nil || args.command != "sync" || args.configPath != "custom.toml" || !args.allowSecrets || len(args.positionals) != 1 || args.positionals[0] != "editor" {
			t.Fatalf("parse %v: %#v, %v", argv, args, err)
		}
	}
	for _, argv := range [][]string{{"--config"}, {"--config="}, {"--config", "--help"}, {"--config", "-h"}, {"sync", "--unknown"}} {
		if _, err := parseArgs(argv); err == nil {
			t.Fatalf("invalid arguments accepted: %v", argv)
		}
	}
}

func TestHelpExplainsAllowlist(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help: exit %d: %s", code, stderr.String())
	}
	for _, text := range []string{"GitHub", "parity sync", "*\n  !.gitignore\n  !config.toml", "!themes/", "git rm --cached", "last matching"} {
		if !strings.Contains(stdout.String(), text) {
			t.Errorf("help is missing %q", text)
		}
	}
}

func TestSyncCLIProgressAndSummary(t *testing.T) {
	repos := newTestRepos(t)
	t.Setenv("PARITY_STATE_DIR", t.TempDir())
	writeTestFile(t, filepath.Join(repos.local, "config.toml"), "theme = 'dark'")
	configPath := filepath.Join(t.TempDir(), "parity.toml")
	writeTestFile(t, configPath, "[editor]\nlocal_dir = '"+repos.local+"'\n[missing]\nlocal_dir = '/parity-missing-directory'\n")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--config", configPath, "sync"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("mixed outcomes should exit 1, got %d", code)
	}
	for _, text := range []string{"Syncing 2 folders", "[1/2] editor", "Pulling remote changes", "Checking selected files", "Committing local changes", "Pushing to remote", "✓ editor — synced", "1 synced", "1 failed"} {
		if !strings.Contains(stdout.String(), text) {
			t.Errorf("stdout missing %q: %s", text, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "\x1b") || !strings.Contains(stderr.String(), "✗ missing — sync failed") {
		t.Fatalf("bad nonterminal output: stdout=%q, stderr=%q", stdout.String(), stderr.String())
	}
	stateDir, _ := parityDir()
	state, err := readState(stateDir)
	if err != nil || state.Entries["editor"].Result != "ok" || state.Entries["missing"].Result != "error" {
		t.Fatalf("state: %#v, %v", state, err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"sync", "editor", "--config", configPath}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "1 up to date") {
		t.Fatalf("second sync: exit=%d, stdout=%s, stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestCLISecretsAndInvalidCommands(t *testing.T) {
	repos := newTestRepos(t)
	t.Setenv("PARITY_STATE_DIR", t.TempDir())
	writeTestFile(t, filepath.Join(repos.local, "config.toml"), "api_key = 'abcdefghijklmnopqr'")
	configPath := filepath.Join(t.TempDir(), "parity.toml")
	writeTestFile(t, configPath, "[editor]\nlocal_dir = '"+repos.local+"'\n")
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"sync", "--config", configPath}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "1 blocked by secrets") || !strings.Contains(stderr.String(), "commit and push skipped") {
		t.Fatalf("blocked sync: exit=%d, stdout=%s, stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, argv := range [][]string{{"invalid"}, {"sync", "unknown", "--config", configPath}, {"list", "unexpected", "--config", configPath}} {
		stdout.Reset()
		stderr.Reset()
		if code := Run(context.Background(), argv, &stdout, &stderr); code != 1 || stderr.Len() == 0 {
			t.Errorf("invalid command %v: exit=%d, stderr=%s", argv, code, stderr.String())
		}
	}
}

func TestListStatusAndStopWithoutWatcher(t *testing.T) {
	t.Setenv("PARITY_STATE_DIR", t.TempDir())
	configPath := filepath.Join(t.TempDir(), "parity.toml")
	writeTestFile(t, configPath, "[editor]\nlocal_dir = '/tmp/editor'\n")
	for _, test := range []struct {
		argv []string
		code int
		text string
	}{
		{[]string{"list", "--config", configPath}, 0, "/tmp/editor"},
		{[]string{"status"}, 1, "not running"},
		{[]string{"stop"}, 0, "not running"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), test.argv, &stdout, &stderr); code != test.code || !strings.Contains(stdout.String(), test.text) {
			t.Errorf("%v: exit=%d, stdout=%s, stderr=%s", test.argv, code, stdout.String(), stderr.String())
		}
	}
}

func TestSyncCLIStopsWhenCredentialsAreMissing(t *testing.T) {
	repos := newTestRepos(t)
	t.Setenv("PARITY_STATE_DIR", t.TempDir())
	private := filepath.Join(t.TempDir(), "private")
	testGit(t, filepath.Dir(private), "init", "-q", "-b", "master", private)
	testGit(t, private, "remote", "add", "origin", "ssh://example.invalid/private.git")
	// Git must not give SSH a terminal to prompt on.
	testGit(t, private, "config", "core.sshCommand", "sh -c 'read answer </dev/tty && echo prompted >&2; echo \"git@example.invalid: Permission denied (publickey).\" >&2; exit 255'")
	writeTestFile(t, filepath.Join(repos.local, "config.toml"), "theme = 'dark'")
	configPath := filepath.Join(t.TempDir(), "parity.toml")
	writeTestFile(t, configPath, "[private]\nlocal_dir = '"+private+"'\n[editor]\nlocal_dir = '"+repos.local+"'\n")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"sync", "--config", configPath}, &stdout, &stderr)
	if code != 1 || strings.Contains(stderr.String(), "prompted") || strings.Contains(stdout.String(), "[2/2]") {
		t.Fatalf("credential failure: exit=%d, stdout=%s, stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, text := range []string{"git pull needs credentials", "Permission denied (publickey)", "Stopped before syncing 1 more folder.", "ssh-add", "gh auth setup-git"} {
		if !strings.Contains(stderr.String(), text) {
			t.Errorf("stderr missing %q: %s", text, stderr.String())
		}
	}
	if result := git(context.Background(), repos.bare, "rev-parse", "--verify", "HEAD"); result.code == 0 {
		t.Fatal("folders after the credential failure must not sync")
	}
	if !isAuthFailure("fatal: could not read Username for 'https://github.com': terminal prompts disabled") {
		t.Fatal("HTTPS prompt failure not detected")
	}
}

func TestVersion(t *testing.T) {
	for _, argv := range [][]string{{"--version"}, {"version"}} {
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), argv, &stdout, &stderr); code != 0 || stdout.String() != "parity dev\n" {
			t.Fatalf("%v: exit=%d, stdout=%q, stderr=%q", argv, code, stdout.String(), stderr.String())
		}
	}
}
