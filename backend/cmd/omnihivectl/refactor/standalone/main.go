package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/cmd/omnihivectl/refactor"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := refactor.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
