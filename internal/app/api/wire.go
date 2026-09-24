// Package api 负责 HTTP 配置、依赖装配和生命周期。
package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/example/go-starter-kit/internal/httpapi"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/example/go-starter-kit/internal/modules/account"
	"github.com/example/go-starter-kit/internal/modules/project"
	"github.com/example/go-starter-kit/internal/modules/task"
)

// Dependencies 显式列出应用的运行时依赖，便于测试和组合。
type Dependencies struct {
	Projects      *project.Service
	Tasks         *task.Service
	Accounts      *account.Service
	Authenticator identity.Authenticator
	Ready         func(context.Context) error
}

// Database 是应用装配所需的数据库能力，生产与测试使用同一构造路径。
type Database interface {
	account.Database
	Ping(context.Context) error
}

// NewServices 构造普通业务依赖，不访问在线数据库；生命周期由调用者管理。
func NewServices(cfg Config, database Database, logger *slog.Logger) (Dependencies, error) {
	if database == nil || logger == nil {
		return Dependencies{}, fmt.Errorf("应用依赖不完整")
	}
	accounts, err := newAccounts(cfg, database, account.NewAdminChecker(database), logger)
	if err != nil {
		return Dependencies{}, err
	}
	authenticator, err := newAuthenticator(accounts)
	if err != nil {
		return Dependencies{}, err
	}
	return Dependencies{Projects: project.NewService(database), Tasks: task.NewService(database), Accounts: accounts, Authenticator: authenticator, Ready: database.Ping}, nil
}

func newAuthenticator(accounts *account.Service) (identity.Authenticator, error) {
	if accounts == nil {
		return nil, fmt.Errorf("缺少账户服务")
	}
	return sessionAuthenticator{accounts}, nil
}

type sessionAuthenticator struct{ accounts *account.Service }

func (a sessionAuthenticator) Authenticate(ctx context.Context, raw string) (identity.Principal, error) {
	user, session, e := a.accounts.AuthenticateSession(ctx, raw)
	return identity.Principal{Subject: user, SessionID: session}, e
}

// NewHandler 只构造可服务的运行时应用；缺少必要依赖时立即报错。
func NewHandler(cfg Config, logger *slog.Logger, deps Dependencies) (http.Handler, huma.API, error) {
	if logger == nil || deps.Projects == nil || deps.Tasks == nil || deps.Accounts == nil || deps.Authenticator == nil || deps.Ready == nil {
		return nil, nil, fmt.Errorf("incomplete application dependencies")
	}
	handler, api := httpapi.New(httpapi.Config{RequestTimeout: cfg.RequestTimeout, DocsEnabled: cfg.DocsEnabled}, logger)
	for _, module := range routeCatalog() {
		module.bind(api, deps)
	}
	return handler, api, nil
}
