// Package app 负责配置、依赖装配和应用生命周期。
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/example/go-starter-kit/internal/httpapi"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/example/go-starter-kit/internal/modules/project"
	"github.com/example/go-starter-kit/internal/modules/task"
)

// Dependencies 显式列出应用的运行时依赖，便于测试和组合。
type Dependencies struct {
	Projects      *project.Service
	Tasks         *task.Service
	Authenticator identity.Authenticator
	Ready         func(context.Context) error
}

func NewDependencies(pool *pgxpool.Pool, authenticator identity.Authenticator, ready func(context.Context) error) Dependencies {
	if pool == nil {
		return Dependencies{Authenticator: authenticator, Ready: ready}
	}
	return Dependencies{Projects: project.NewService(pool), Tasks: task.NewService(pool), Authenticator: authenticator, Ready: ready}
}

func newAuthenticator(ctx context.Context, cfg Config) (identity.Authenticator, error) {
	switch cfg.AuthMode {
	case "dev":
		if err := validateAuthEnvironment(cfg.Environment, cfg.AuthMode); err != nil {
			return nil, err
		}
		return identity.NewDevelopment(cfg.DevToken, cfg.DevSubject)
	case "jwt":
		return identity.NewJWT(ctx, identity.JWTConfig{Issuer: cfg.AuthIssuer, Audience: cfg.AuthAudience, JWKSURL: cfg.AuthJWKSURL})
	default:
		return nil, fmt.Errorf("unsupported authentication mode")
	}
}

// NewHandler 只构造可服务的运行时应用；缺少必要依赖时立即报错。
func NewHandler(cfg Config, logger *slog.Logger, deps Dependencies) (http.Handler, huma.API, error) {
	if logger == nil || deps.Projects == nil || deps.Tasks == nil || deps.Authenticator == nil || deps.Ready == nil {
		return nil, nil, fmt.Errorf("incomplete application dependencies")
	}
	handler, api := httpapi.New(httpapi.Config{RequestTimeout: cfg.RequestTimeout, DocsEnabled: cfg.DocsEnabled}, logger)
	for _, module := range routeCatalog() {
		module.bind(api, deps)
	}
	return handler, api, nil
}
