package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/example/go-starter-kit/internal/httpapi"
	"github.com/example/go-starter-kit/internal/modules/account"
	"github.com/example/go-starter-kit/internal/modules/project"
	"github.com/example/go-starter-kit/internal/modules/task"
)

type moduleRoutes struct {
	bind     func(huma.API, Dependencies)
	describe func(huma.API)
}

// routeCatalog 是运行时和离线契约共用的路由清单，新增模块只需在此装配。
func routeCatalog() []moduleRoutes {
	return []moduleRoutes{
		module(httpapi.HealthRoutes(), func(d Dependencies) func(context.Context) error { return d.Ready }),
		module(account.Routes(), func(d Dependencies) *account.Service { return d.Accounts }),
		module(account.AdminRoutes(), func(d Dependencies) *account.Service { return d.Accounts }),
		module(project.Routes(), func(d Dependencies) *project.Service { return d.Projects }),
		module(task.Routes(), func(d Dependencies) *task.Service { return d.Tasks }),
	}
}

func module[S any](routes []httpapi.Route[S], selectService func(Dependencies) S) moduleRoutes {
	return moduleRoutes{
		bind: func(api huma.API, deps Dependencies) {
			for _, route := range routes {
				var middlewares huma.Middlewares
				switch route.Policy() {
				case httpapi.Session:
					middlewares = append(middlewares, httpapi.Middleware(api, deps.Authenticator))
				case httpapi.Public:
				default:
					panic("未注册认证策略")
				}
				route.Bind(api, selectService(deps), middlewares...)
			}
		},
		describe: func(api huma.API) {
			for _, route := range routes {
				route.Describe(api)
			}
		},
	}
}

// OpenAPI 在不加载环境、数据库、密钥和第三方服务的情况下导出接口契约。
func OpenAPI() ([]byte, error) {
	_, api := httpapi.New(httpapi.Config{RequestTimeout: 15 * time.Second}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, module := range routeCatalog() {
		module.describe(api)
	}
	return json.MarshalIndent(api.OpenAPI(), "", "  ")
}
