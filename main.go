package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/go-starter-kit/internal/app"
	"github.com/example/go-starter-kit/internal/db"
)

func main() {
	if err := run(); err != nil {
		slog.Error("命令执行失败", "error", err)
		os.Exit(1)
	}
}

func run() error {
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	switch command {
	case "openapi":
		data, err := app.OpenAPI()
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(append(data, '\n'))
		return err
	case "migrate":
		if len(os.Args) != 3 || (os.Args[2] != "up" && os.Args[2] != "down") {
			return fmt.Errorf("usage: api migrate up|down")
		}
		url := os.Getenv("DATABASE_URL")
		if url == "" {
			return fmt.Errorf("DATABASE_URL is required")
		}
		ctx, cancel := context.WithTimeout(signalCtx, 2*time.Minute)
		defer cancel()
		return db.Migrate(ctx, url, os.Args[2])
	case "serve":
		return app.Run(signalCtx)
	default:
		return fmt.Errorf("usage: api [serve|openapi|migrate up|migrate down]")
	}
}
