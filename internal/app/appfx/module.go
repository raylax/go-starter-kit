// Package appfx 提供 API 与 Worker 共用的 Fx 基础设施装配。
package appfx

import (
	"context"
	"log/slog"
	"os"

	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/platform/telemetry"
	"go.uber.org/fx"
)

type telemetryReady struct{}

var Module = fx.Module("infrastructure",
	fx.Provide(newLogger, newTelemetry, newDatabase),
)

func newLogger(cfg Config) *slog.Logger {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	return logger
}

func newTelemetry(lc fx.Lifecycle, cfg Config, _ *slog.Logger) *telemetryReady {
	var shutdown func(context.Context) error
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			var err error
			shutdown, err = telemetry.Setup(ctx, cfg.OTelServiceName, cfg.OTelEnabled)
			return err
		},
		OnStop: func(ctx context.Context) error { return shutdown(ctx) },
	})
	return &telemetryReady{}
}

func newDatabase(lc fx.Lifecycle, cfg Config, _ *telemetryReady) *Database {
	database := &Database{}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			var err error
			database.pool, err = db.Open(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
			return err
		},
		OnStop: func(ctx context.Context) error {
			closed := make(chan struct{})
			go func() { database.pool.Close(); close(closed) }()
			select {
			case <-closed:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
	return database
}
