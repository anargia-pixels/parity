package parity

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type syncProgress struct {
	mu       sync.Mutex
	writer   io.Writer
	steps    []string
	step     int
	frame    int
	prefix   string
	terminal bool
	stop     chan struct{}
	done     chan struct{}
}

func isTerminal(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	stat, err := file.Stat()
	return err == nil && stat.Mode()&os.ModeCharDevice != 0
}

func newSyncProgress(writer io.Writer, label string, position, total int) *syncProgress {
	if writer == nil {
		return nil
	}
	bar := &syncProgress{
		writer:   writer,
		steps:    []string{"Pulling remote changes", "Checking selected files", "Committing local changes", "Pushing to remote"},
		prefix:   fmt.Sprintf("  [%d/%d] %s", position, total, label),
		terminal: isTerminal(writer),
	}
	if !bar.terminal {
		return bar
	}
	bar.stop, bar.done = make(chan struct{}), make(chan struct{})
	go func() {
		defer close(bar.done)
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-bar.stop:
				return
			case <-ticker.C:
				bar.mu.Lock()
				bar.frame++
				bar.render()
				bar.mu.Unlock()
			}
		}
	}()
	return bar
}

func (bar *syncProgress) setStep(step int) {
	if bar == nil {
		return
	}
	bar.mu.Lock()
	defer bar.mu.Unlock()
	bar.step = step
	bar.render()
}

func (bar *syncProgress) render() {
	line := fmt.Sprintf("%s · %s (%d/%d)", bar.prefix, bar.steps[bar.step], bar.step+1, len(bar.steps))
	if bar.terminal {
		fmt.Fprintf(bar.writer, "\r\x1b[2K%s %s", line, spinnerFrames[bar.frame%len(spinnerFrames)])
	} else {
		fmt.Fprintln(bar.writer, line)
	}
}

func (bar *syncProgress) finish() {
	if bar == nil || !bar.terminal {
		return
	}
	close(bar.stop)
	<-bar.done
	fmt.Fprint(bar.writer, "\r\x1b[2K")
}
