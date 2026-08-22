package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/agentapp"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := agentapp.Run(ctx); err != nil && ctx.Err() == nil {
		slog.Error("Wrapper agent stopped", "error", err)
		os.Exit(1)
	}
}
