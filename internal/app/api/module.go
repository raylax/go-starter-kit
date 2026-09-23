package api

import (
	"github.com/example/go-starter-kit/internal/authorization"
	"log/slog"

	"github.com/example/go-starter-kit/internal/app/appfx"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/example/go-starter-kit/internal/modules/account"
	"github.com/example/go-starter-kit/internal/modules/project"
	"github.com/example/go-starter-kit/internal/modules/task"
	"go.uber.org/fx"
)

// Module 只装配 API 需要的服务，业务构造函数无需依赖 Fx。
var Module = fx.Module("api",
	fx.Provide(
		newAuthorizer,
		func(cfg Config, database *appfx.Database, authorizer authorization.Authorizer) (*account.Service, error) {
			return newAccounts(cfg, database, authorizer)
		},
		func(database *appfx.Database) *project.Service { return project.NewService(database) },
		func(database *appfx.Database) *task.Service { return task.NewService(database) },
		func(accounts *account.Service) (identity.Authenticator, error) {
			return newAuthenticator(accounts)
		},
		newHTTPService,
	),
	fx.Invoke(func(*httpService) {}),
)

// httpDependencies 显式声明 HTTP 服务的依赖，限制 Fx 类型只出现在装配层。
type httpDependencies struct {
	fx.In
	Lifecycle     fx.Lifecycle
	Config        Config
	Logger        *slog.Logger
	Database      *appfx.Database
	Accounts      *account.Service
	Projects      *project.Service
	Tasks         *task.Service
	Authenticator identity.Authenticator
	Failures      *appfx.Failures
}
