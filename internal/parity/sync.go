package parity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Outcome statuses are stored in state.json. Reasons refine a status.
const (
	statusOK      = "ok"
	statusSkipped = "skipped"
	statusError   = "error"

	reasonEmpty   = "empty"   // skipped: nothing to commit or push
	reasonSecrets = "secrets" // skipped: staged changes look like secrets
	reasonLocked  = "locked"  // error: another sync holds the folder lock
	reasonAuth    = "auth"    // error: Git needs credentials it cannot prompt for
)

type syncOutcome struct {
	status  string
	reason  string
	files   []string
	message string
}

type syncOptions struct {
	stateDir     string
	allowSecrets bool
	// stopOnAuth ends syncAll after the first folder that needs credentials.
	stopOnAuth bool
	progress   io.Writer
	position   int
	total      int
}

type entryOutcome struct {
	label   string
	outcome syncOutcome
}

func timestamp() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

func syncEntry(ctx context.Context, item entry, options syncOptions) syncOutcome {
	stat, err := os.Stat(item.localDir)
	if err != nil || !stat.IsDir() {
		return syncError("directory does not exist: " + item.localDir)
	}
	repo := git(ctx, item.localDir, "rev-parse", "--is-inside-work-tree")
	if repo.code != 0 || strings.TrimSpace(repo.stdout) != "true" {
		return syncError("not a git repository: " + item.localDir)
	}
	remote := git(ctx, item.localDir, "remote")
	if remote.code != 0 || strings.TrimSpace(remote.stdout) == "" {
		return syncError("no git remote configured in: " + item.localDir)
	}
	lock, err := acquireSyncLock(ctx, options.stateDir, item.label)
	if errors.Is(err, errLocked) {
		return syncOutcome{status: statusError, reason: reasonLocked, message: "sync already in progress for " + item.label}
	}
	if err != nil {
		return syncError("could not lock " + item.label + ": " + err.Error())
	}
	defer lock.release()
	bar := newSyncProgress(options.progress, item.label, options.position, options.total)
	defer bar.finish()
	return doSync(ctx, item, options, bar)
}

func doSync(ctx context.Context, item entry, options syncOptions, bar *syncProgress) syncOutcome {
	dir := item.localDir
	bar.setStep(0)
	beforePull := git(ctx, dir, "rev-parse", "--verify", "HEAD")
	pull := git(ctx, dir, "pull")
	if pull.code != 0 && isAuthFailure(pull.stderr) {
		return remoteError("pull", pull.stderr)
	}
	if pull.code != 0 && !hasNoUpstream(pull.stderr) {
		if err := stashPullPop(ctx, item, pull.stderr); err != nil {
			return syncError(err.Error())
		}
	}
	bar.setStep(1)
	add := git(ctx, dir, "add", "-A", ".")
	if add.code != 0 {
		return syncError("git add failed: " + add.stderr)
	}
	staged := git(ctx, dir, "diff", "--cached", "--quiet")
	if staged.code != 0 && staged.code != 1 {
		return syncError("git diff failed: " + staged.stderr)
	}
	if staged.code == 0 {
		// A previous push may have failed after a successful commit.
		head := git(ctx, dir, "rev-parse", "--verify", "HEAD")
		if head.code == 0 {
			pending := git(ctx, dir, "rev-list", "--count", "@{upstream}..HEAD")
			bar.setStep(3)
			if push := git(ctx, dir, "push"); push.code != 0 {
				return remoteError("push", push.stderr)
			}
			if head.stdout != beforePull.stdout || pending.code != 0 || strings.TrimSpace(pending.stdout) != "0" {
				return syncOutcome{status: statusOK}
			}
		}
		return syncOutcome{status: statusSkipped, reason: reasonEmpty}
	}
	if !options.allowSecrets {
		findings, err := checkStagedForSecrets(ctx, dir)
		if err != nil {
			return syncError(err.Error())
		}
		if len(findings) > 0 {
			return secretsOutcome(findings)
		}
	}
	host, err := os.Hostname()
	if err != nil {
		return syncError("get hostname: " + err.Error())
	}
	bar.setStep(2)
	args := append(identityFlags(ctx, dir), "commit", "-m", "sync "+timestamp()+" "+host)
	commit := git(ctx, dir, args...)
	if commit.code != 0 {
		return syncError("git commit failed: " + commit.stderr)
	}
	bar.setStep(3)
	push := git(ctx, dir, "push")
	if push.code != 0 {
		return remoteError("push", push.stderr)
	}
	return syncOutcome{status: statusOK}
}

// secretsOutcome lists each flagged file once and every finding in the message.
func secretsOutcome(findings []secretFinding) syncOutcome {
	seen := make(map[string]bool)
	var files, details []string
	for _, finding := range findings {
		if !seen[finding.file] {
			files = append(files, finding.file)
			seen[finding.file] = true
		}
		details = append(details, fmt.Sprintf("  %s (%s)", finding.file, finding.pattern))
	}
	return syncOutcome{status: statusSkipped, reason: reasonSecrets, files: files,
		message: "possible secrets staged:\n" + strings.Join(details, "\n")}
}

func hasNoUpstream(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "no such ref was fetched") || strings.Contains(message, "no tracking information")
}

// stashPullPop restores local edits after pulling and keeps a conflicting stash.
func stashPullPop(ctx context.Context, item entry, firstError string) error {
	dir := item.localDir
	status := git(ctx, dir, "status", "--porcelain")
	if status.code != 0 {
		return fmt.Errorf("git pull failed and status check failed: %s", status.stderr)
	}
	if strings.TrimSpace(status.stdout) == "" {
		return fmt.Errorf("git pull failed: %s", firstError)
	}
	args := append(identityFlags(ctx, dir), "stash", "push", "--include-untracked", "-m", "parity autostash "+item.label)
	stash := git(ctx, dir, args...)
	if stash.code != 0 {
		return fmt.Errorf("git pull failed and could not stash local changes: %s", stash.stderr)
	}
	pull := git(ctx, dir, "pull")
	if ctx.Err() != nil {
		return fmt.Errorf("git pull interrupted — stash preserved: %w", ctx.Err())
	}
	pop := git(ctx, dir, "stash", "pop")
	if pop.code != 0 {
		if pull.code != 0 {
			return fmt.Errorf("git pull failed and stash pop also failed — stash preserved: %s", pop.stderr)
		}
		return fmt.Errorf("merge conflict after git pull — stash preserved (resolve with \"git stash pop\" in %s): %s", dir, pop.stderr)
	}
	if pull.code != 0 {
		return fmt.Errorf("git pull failed (local changes restored): %s", pull.stderr)
	}
	return nil
}

func syncError(message string) syncOutcome {
	return syncOutcome{status: statusError, message: message}
}

func remoteError(action, stderr string) syncOutcome {
	if isAuthFailure(stderr) {
		return syncOutcome{status: statusError, reason: reasonAuth, message: "git " + action + " needs credentials: " + stderr}
	}
	return syncError("git " + action + " failed: " + stderr)
}

func outcomeMessage(outcome syncOutcome) string {
	switch outcome.status {
	case statusSkipped:
		return outcome.reason
	case statusError:
		return outcome.message
	default:
		return "synced"
	}
}

func syncAll(ctx context.Context, entries []entry, options syncOptions, report func(entryOutcome)) ([]entryOutcome, error) {
	outcomes := make([]entryOutcome, 0, len(entries))
	updates := make(map[string]entryState, len(entries))
	for index, item := range entries {
		if ctx.Err() != nil {
			return outcomes, ctx.Err()
		}
		options.position, options.total = index+1, len(entries)
		outcome := syncEntry(ctx, item, options)
		result := entryOutcome{item.label, outcome}
		outcomes = append(outcomes, result)
		report(result)
		updates[item.label] = stateUpdate(outcome)
		if options.stopOnAuth && outcome.reason == reasonAuth {
			break
		}
	}
	return outcomes, writeState(ctx, options.stateDir, updates)
}
