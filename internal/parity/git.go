package parity

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

type gitResult struct {
	code   int
	stdout string
	stderr string
}

func git(ctx context.Context, dir string, args ...string) gitResult {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	// A new session has no terminal, so SSH and HTTPS credential prompts fail
	// instead of waiting for input. See isAuthFailure.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0", "SSH_ASKPASS_REQUIRE=never")
	// Stop Git and its helpers together when parity is canceled.
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := gitResult{stdout: stdout.String(), stderr: strings.TrimSpace(stderr.String())}
	if err != nil {
		result.code = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.code = exitErr.ExitCode()
		}
		if ctx.Err() != nil {
			result.stderr = ctx.Err().Error()
		} else if result.stderr == "" {
			result.stderr = err.Error()
		}
	}
	return result
}

func identityFlags(ctx context.Context, dir string) []string {
	name := git(ctx, dir, "config", "--get", "user.name")
	email := git(ctx, dir, "config", "--get", "user.email")
	if name.code == 0 && email.code == 0 {
		return nil
	}
	return []string{"-c", "user.name=parity", "-c", "user.email=parity@localhost"}
}

var authFailureMessages = []string{
	"terminal prompts disabled",
	"could not read username",
	"could not read password",
	"authentication failed",
	"permission denied (publickey",
	"host key verification failed",
}

// isAuthFailure reports whether Git failed because the remote needs
// credentials that Git could not supply without a prompt.
func isAuthFailure(stderr string) bool {
	stderr = strings.ToLower(stderr)
	for _, message := range authFailureMessages {
		if strings.Contains(stderr, message) {
			return true
		}
	}
	return false
}
