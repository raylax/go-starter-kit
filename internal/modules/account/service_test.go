package account

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgconn"
)

type failingAuditDatabase struct{ sqlc.DBTX }

func (failingAuditDatabase) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("private-database-error")
}

func TestAuditFailureUsesInjectedLoggerAndRequestID(t *testing.T) {
	var logs bytes.Buffer
	service := Service{queries: newStore(failingAuditDatabase{}), deps: Dependencies{Logger: slog.New(slog.NewJSONHandler(&logs, nil))}}
	service.recordFailure(t.Context(), Request{RequestID: "request-correlation"}, "auth.login", ErrCredentials)
	if !strings.Contains(logs.String(), "request-correlation") || !strings.Contains(logs.String(), "auth.login") {
		t.Fatal("审计失败缺少请求关联")
	}
	if strings.Contains(logs.String(), "private-database-error") {
		t.Fatal("审计失败泄露底层信息")
	}
}
