package parity

import (
	"context"
	"os"
	"os/exec"
	"time"
)

func notify(summary, body string) {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		exec.CommandContext(ctx, "notify-send", "-a", "parity", "-u", "critical", "--", summary, body).Run()
	}()
}
