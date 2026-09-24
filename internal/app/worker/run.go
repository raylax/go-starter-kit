package worker

import (
	"context"

	"github.com/example/go-starter-kit/internal/app/appfx"
	"go.uber.org/fx"
)

// Run 使用独立 Fx 依赖图启动后台消费者。
func Run(ctx context.Context, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	return appfx.Run(ctx, cfg.infrastructure(), fx.Supply(cfg), Module)
}

func (cfg Config) infrastructure() appfx.Config {
	return appfx.Config{
		DatabaseURL: cfg.DatabaseURL, DBMaxConns: cfg.DBMaxConns, LogLevel: cfg.LogLevel,
		OTelEnabled: cfg.OTelEnabled, OTelServiceName: cfg.OTelServiceName, ShutdownTimeout: cfg.ShutdownTimeout,
	}
}
