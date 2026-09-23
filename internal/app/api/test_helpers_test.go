package api

import (
	"context"
	"github.com/example/go-starter-kit/internal/authorization"
	"github.com/example/go-starter-kit/internal/modules/account"
	"github.com/example/go-starter-kit/internal/platform/password"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/example/go-starter-kit/internal/identity"
	"github.com/example/go-starter-kit/internal/modules/project"
	"github.com/example/go-starter-kit/internal/modules/task"
)

type failingAuthenticator struct{ err error }

func (a failingAuthenticator) Authenticate(context.Context, string) (identity.Principal, error) {
	return identity.Principal{}, a.err
}

// unexpectedDB 让任何意外的数据库访问立即暴露，而不是依赖空指针。
type unexpectedDB struct{}

func (unexpectedDB) Begin(context.Context) (pgx.Tx, error) { panic("unexpected database access") }

func (unexpectedDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected database access")
}
func (unexpectedDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected database access")
}
func (unexpectedDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("unexpected database access")
}

func testHandler(t *testing.T, cfg Config, logger *slog.Logger, authenticator identity.Authenticator, ready func(context.Context) error) http.Handler {
	t.Helper()
	deps := testDependencies()
	deps.Authenticator, deps.Ready = authenticator, ready
	handler, _, err := NewHandler(cfg, logger, deps)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func testDependencies() Dependencies {
	hasher, e := password.New(1)
	if e != nil {
		panic(e)
	}
	accounts, e := account.NewService(unexpectedDB{}, account.Options{IdleTTL: 30 * time.Minute, MaxTTL: 24 * time.Hour}, account.Dependencies{Authorizer: authorization.AdminCheckFunc(account.NewAdminChecker(unexpectedDB{}).CheckAdmin), Hash: hasher.Hash, Verify: hasher.Verify, ValidPassword: password.Validate})
	if e != nil {
		panic(e)
	}
	return Dependencies{Projects: project.NewService(unexpectedDB{}), Tasks: task.NewService(unexpectedDB{}), Accounts: accounts, Authenticator: failingAuthenticator{identity.ErrUnauthorized}, Ready: func(context.Context) error { return nil }}
}
