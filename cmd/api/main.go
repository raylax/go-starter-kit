package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/example/go-starter-kit/internal/app/api"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := api.NewCommand().ExecuteContext(ctx); err != nil {
		slog.Error("命令执行失败", "error", err)
		os.Exit(1)
	}
}
