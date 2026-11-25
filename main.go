package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/anargia-pixels/parity/internal/parity"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := parity.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
