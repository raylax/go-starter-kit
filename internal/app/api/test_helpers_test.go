package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/example/go-starter-kit/internal/identity"
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
	cfg := Config{AuthSessionIdleTTL: 30 * time.Minute, AuthSessionMaxTTL: 24 * time.Hour, FrontendURL: "https://web.example"}
	deps, err := NewServices(cfg, unexpectedDB{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		panic(err)
	}
	deps.Authenticator = failingAuthenticator{identity.ErrUnauthorized}
	deps.Ready = func(context.Context) error { return nil }
	return deps
}

func (unexpectedDB) Ping(context.Context) error { panic("unexpected database access") }
