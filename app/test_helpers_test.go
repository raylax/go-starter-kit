package app

import (
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/example/go-starter-kit/identity"
	"github.com/example/go-starter-kit/modules/project"
	"github.com/example/go-starter-kit/modules/task"
)

type failingAuthenticator struct{ err error }

func (a failingAuthenticator) Authenticate(context.Context, string) (string, error) { return "", a.err }

// unexpectedDB 让任何意外的数据库访问立即暴露，而不是依赖空指针。
type unexpectedDB struct{}

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
	return Dependencies{Projects: project.NewService(unexpectedDB{}), Tasks: task.NewService(unexpectedDB{}), Authenticator: failingAuthenticator{identity.ErrUnauthorized}, Ready: func(context.Context) error { return nil }}
}
