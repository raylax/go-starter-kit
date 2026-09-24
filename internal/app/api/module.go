package api

import (
	"log/slog"

	"github.com/example/go-starter-kit/internal/app/appfx"
	"go.uber.org/fx"
)

// Module 只装配 API 需要的服务，业务构造函数无需依赖 Fx。
var Module = fx.Module("api",
	fx.Provide(
		func(cfg Config, database *appfx.Database, logger *slog.Logger) (Dependencies, error) {
			return NewServices(cfg, database, logger)
		},
		newHTTPService,
	),
	fx.Invoke(func(*httpService) {}),
)

// httpDependencies 显式声明 HTTP 服务的依赖，限制 Fx 类型只出现在装配层。
type httpDependencies struct {
	fx.In
	Lifecycle fx.Lifecycle
	Config    Config
	Logger    *slog.Logger
	Database  *appfx.Database
	Services  Dependencies
	Failures  *appfx.Failures
}
