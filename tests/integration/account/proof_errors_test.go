//go:build integration

package account_test

import (
	"context"
	"errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/example/go-starter-kit/internal/httpapi"
	"github.com/example/go-starter-kit/internal/modules/account"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestConcurrentEmailProofReturnsReauthenticationError(t *testing.T) {
	f := newFixture(t)
	session := f.register("email-race@example.com")
	r := fixtureSubject(t, f, session.Token)
	proof, err := f.s.Reauthenticate(t.Context(), r, account.Reauthentication{Method: account.MethodPassword, Password: testPassword, Operation: account.OperationChangeEmail, Target: "email-race-new@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	// 暂停认领更新，让两个请求先通过证明校验，再制造确定性的认领竞争。
	tx, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(t.Context(), "SELECT id FROM auth_flows WHERE id=$1 FOR UPDATE", proof.ReauthenticationID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	outcomes := make(chan error, 2)
	for range 2 {
		go func() { outcomes <- f.s.ChangeEmail(ctx, r, "email-race-new@example.com", proof.ReauthenticationID) }()
	}
	for {
		var waiting int
		err = f.pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE '-- name: ClaimAuthorization%'").Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	failures := 0
	for range 2 {
		err = <-outcomes
		if err == nil {
			continue
		}
		failures++
		var status huma.StatusError
		mapped := httpapi.FromError(ctx, err)
		if !errors.As(mapped, &status) {
			t.Fatal(mapped)
		}
		if status.GetStatus() != http.StatusUnprocessableEntity || !errors.Is(err, account.ErrReauthentication) {
			t.Errorf("证明认领竞争应返回 422，实际为 %d", status.GetStatus())
		}
	}
	if failures != 1 {
		t.Fatalf("应恰有一个请求因证明竞争失败，实际为 %d", failures)
	}
	var challenges, messages int
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM auth_verifications WHERE user_id=$1 AND purpose='change_email'", r.UserID).Scan(&challenges); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM mail_outbox WHERE kind='change_email' AND recipient='email-race-new@example.com'").Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if challenges != 1 || messages != 1 {
		t.Errorf("竞争失败事务未回滚: challenges=%d mails=%d", challenges, messages)
	}
	if _, _, err := f.s.AuthenticateSession(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
}

// 仅在查询已绑定账号时注入故障，其余事务和会话校验仍访问真实数据库。
type accountLookupFailureDatabase struct {
	account.Database
	cause error
}

func (d accountLookupFailureDatabase) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := d.Database.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return accountLookupFailureTx{Tx: tx, cause: d.cause}, nil
}

type accountLookupFailureTx struct {
	pgx.Tx
	cause error
}

func (tx accountLookupFailureTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if strings.HasPrefix(query, "-- name: FindProviderAccount ") {
		return accountLookupFailureRow{tx.cause}
	}
	return tx.Tx.QueryRow(ctx, query, args...)
}

type accountLookupFailureRow struct{ cause error }

func (r accountLookupFailureRow) Scan(...any) error { return r.cause }

func TestConfirmLinkRetryPreservesLookupErrors(t *testing.T) {
	f := newFixture(t)
	session := f.register("confirm-retry@example.com")
	r := fixtureSubject(t, f, session.Token)
	proof := f.reauth(session.Token, testPassword, "link_account", "demo")
	flow := decode[account.AuthFlowResponse](t, f.call(http.MethodPost, "/v1/me/accounts/link", session.Token, map[string]any{"provider": "demo", "reauthentication_id": proof}, http.StatusOK))
	f.callback(flow, "confirm-retry-social", http.StatusOK)
	if err := f.s.ConfirmLink(t.Context(), r, flow.FlowID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		cause  error
		status int
	}{
		{"查询超时", context.DeadlineExceeded, http.StatusGatewayTimeout},
		{"存储故障", errors.New("数据库连接中断"), http.StatusInternalServerError},
		{"账号不存在", pgx.ErrNoRows, http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := f.newService(accountLookupFailureDatabase{Database: f.pool, cause: tc.cause}, f.deps)
			err := service.ConfirmLink(t.Context(), r, flow.FlowID)
			var status huma.StatusError
			if !errors.As(httpapi.FromError(t.Context(), err), &status) || status.GetStatus() != tc.status {
				t.Fatalf("查询失败响应应为 %d，实际错误: %v", tc.status, err)
			}
			if tc.cause == pgx.ErrNoRows {
				if !errors.Is(err, account.ErrFlow) {
					t.Fatalf("账号不存在应使证明失效: %v", err)
				}
			} else if !errors.Is(err, tc.cause) {
				t.Fatalf("查询失败丢失原始原因: %v", err)
			}
		})
	}
	if err := f.s.ConfirmLink(t.Context(), r, flow.FlowID); err != nil {
		t.Fatalf("故障恢复后重试失败: %v", err)
	}
}
