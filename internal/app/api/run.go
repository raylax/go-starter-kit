package api

import (
	"context"

	"github.com/example/go-starter-kit/internal/app/appfx"
	"go.uber.org/fx"
)

// Run 使用 Fx 构建 API 依赖图；配置解析、迁移和契约导出保持独立。
func Run(ctx context.Context, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	return appfx.Run(ctx, appfx.Config{
		DatabaseURL: cfg.DatabaseURL, DBMaxConns: cfg.DBMaxConns, LogLevel: cfg.LogLevel,
		OTelEnabled: cfg.OTelEnabled, OTelServiceName: cfg.OTelServiceName, ShutdownTimeout: cfg.ShutdownTimeout,
	}, fx.Supply(cfg), Module)
}
