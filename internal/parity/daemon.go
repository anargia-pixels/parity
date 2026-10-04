package parity

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func daemonPID(stateDir string) int {
	data, err := os.ReadFile(filepath.Join(stateDir, "parity.pid"))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || !isPIDAlive(pid) {
		return 0
	}
	return pid
}

func startDaemon(ctx context.Context, configPath, stateDir string, stdout io.Writer) error {
	if pid := daemonPID(stateDir); pid != 0 {
		return fmt.Errorf("parity watch is already running (pid %d)", pid)
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(stateDir, "parity.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	defer reader.Close()
	defer writer.Close()
	cmd := exec.Command(executable, "watch", "--foreground", "--config", configPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.ExtraFiles = []*os.File{writer}
	cmd.Env = append(os.Environ(), "PARITY_READY_FD=3")
	if err := cmd.Start(); err != nil {
		return err
	}
	writer.Close()
	ready := make(chan string, 1)
	go func() {
		message, _ := io.ReadAll(reader)
		ready <- strings.TrimSpace(string(message))
	}()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	var startErr error
	select {
	case message := <-ready:
		if message == "ready" {
			pid := cmd.Process.Pid
			cmd.Process.Release()
			fmt.Fprintf(stdout, "parity watch started in the background (pid %d)\nlog: %s\n", pid, logFile.Name())
			return nil
		}
		if message == "" {
			message = "watcher exited before startup; see " + logFile.Name()
		}
		startErr = fmt.Errorf("%s", message)
	case <-ctx.Done():
		startErr = ctx.Err()
	case <-timeout.C:
		startErr = fmt.Errorf("watcher did not start in time; see %s", logFile.Name())
	}
	cmd.Process.Kill()
	cmd.Wait()
	return startErr
}

// signalReady uses the inherited pipe only for a watcher started by this CLI.
func signalReady(err error) {
	if os.Getenv("PARITY_READY_FD") != "3" {
		return
	}
	os.Unsetenv("PARITY_READY_FD")
	file := os.NewFile(3, "parity-ready")
	if err == nil {
		fmt.Fprintln(file, "ready")
	} else {
		fmt.Fprintln(file, err)
	}
	file.Close()
}

func stopWatcher(ctx context.Context, stateDir string, stdout io.Writer) error {
	pid := daemonPID(stateDir)
	if pid == 0 {
		fmt.Fprintln(stdout, "parity watch is not running")
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("stop watcher: %w", err)
	}
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		// The PID file is removed only after active syncs have stopped.
		if daemonPID(stateDir) != pid || !isPIDAlive(pid) {
			fmt.Fprintf(stdout, "parity watch stopped (pid %d)\n", pid)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return fmt.Errorf("watcher (pid %d) did not stop in time", pid)
		case <-ticker.C:
		}
	}
}
