//go:build integration

package mailoutbox_test

import (
	"context"
	"errors"
	"github.com/example/go-starter-kit/internal/modules/mailoutbox"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/example/go-starter-kit/tests/integration/testutil"
	"github.com/google/uuid"
)

func TestRetryAndClearPayload(t *testing.T) {
	pool, _ := testutil.Database(t)
	q := sqlc.New(pool)
	id := uuid.New()
	if err := q.EnqueueMail(t.Context(), sqlc.EnqueueMailParams{ID: id, Kind: "register", Recipient: "test@example.com", Subject: "test", Body: "private", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	service, err := mailoutbox.NewService(pool, func(_ context.Context, m mailoutbox.Message) error {
		calls++
		if m.ID != id.String() {
			t.Fatal("重试未复用邮件 ID")
		}
		if calls == 1 {
			return errors.New("private provider response")
		}
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ProcessOne(t.Context()); err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessOne(t.Context()); worked || err != nil {
		t.Fatal("重试未遵守等待时间")
	}
	if _, err = pool.Exec(t.Context(), "UPDATE mail_outbox SET available_at=now()"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ProcessOne(t.Context()); err != nil {
		t.Fatal(err)
	}
	var status, recipient, body string
	var attempts int
	if err := pool.QueryRow(t.Context(), "SELECT status, recipient, body, attempts FROM mail_outbox WHERE id=$1", id).Scan(&status, &recipient, &body, &attempts); err != nil {
		t.Fatal(err)
	}
	if mailoutbox.Status(status) != mailoutbox.StatusSent || recipient != "" || body != "" || attempts != 2 {
		t.Fatal("终态或敏感载荷清除错误")
	}
}

func TestLeaseOwnershipAndExpiry(t *testing.T) {
	pool, _ := testutil.Database(t)
	q := sqlc.New(pool)
	id := uuid.New()
	if err := q.EnqueueMail(t.Context(), sqlc.EnqueueMailParams{ID: id, Kind: "test", Recipient: "test@example.com", Subject: "test", Body: "private", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	first, second := uuid.New(), uuid.New()
	if _, err := q.ClaimMail(t.Context(), &first); err != nil {
		t.Fatal(err)
	}
	if _, err := q.ClaimMail(t.Context(), &second); err == nil {
		t.Fatal("同一租约被重复领取")
	}
	if _, err := pool.Exec(t.Context(), "UPDATE mail_outbox SET leased_until=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	if _, err := q.ClaimMail(t.Context(), &second); err != nil {
		t.Fatal(err)
	}
	if n, err := q.CompleteMail(t.Context(), sqlc.CompleteMailParams{ID: id, LeaseID: &first}); err != nil || n != 0 {
		t.Fatal("旧租约覆盖了新状态")
	}
	if _, err := pool.Exec(t.Context(), "UPDATE mail_outbox SET leased_until=now()-interval '1 second', expires_at=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	if err := q.ExpireMail(t.Context()); err != nil {
		t.Fatal(err)
	}
	var status, body string
	if err := pool.QueryRow(t.Context(), "SELECT status,body FROM mail_outbox WHERE id=$1", id).Scan(&status, &body); err != nil {
		t.Fatal(err)
	}
	if mailoutbox.Status(status) != mailoutbox.StatusFailed || body != "" {
		t.Fatal("过期邮件未终止并清除正文")
	}
}
