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
	if _, err := q.ClaimMail(t.Context(), sqlc.ClaimMailParams{LeaseID: &first, MaxAttempts: 5, LeaseSeconds: 60}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.ClaimMail(t.Context(), sqlc.ClaimMailParams{LeaseID: &second, MaxAttempts: 5, LeaseSeconds: 60}); err == nil {
		t.Fatal("同一租约被重复领取")
	}
	if _, err := pool.Exec(t.Context(), "UPDATE mail_outbox SET leased_until=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	if _, err := q.ClaimMail(t.Context(), sqlc.ClaimMailParams{LeaseID: &second, MaxAttempts: 5, LeaseSeconds: 60}); err != nil {
		t.Fatal(err)
	}
	if n, err := q.CompleteMail(t.Context(), sqlc.CompleteMailParams{ID: id, LeaseID: &first}); err != nil || n != 0 {
		t.Fatal("旧租约覆盖了新状态")
	}
	if _, err := pool.Exec(t.Context(), "UPDATE mail_outbox SET leased_until=now()-interval '1 second', expires_at=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	if err := q.ExpireMail(t.Context(), sqlc.ExpireMailParams{MaxAttempts: 5, BatchSize: 100}); err != nil {
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

func TestAttemptLimitClearsPayload(t *testing.T) {
	pool, _ := testutil.Database(t)
	q := sqlc.New(pool)
	for _, tc := range []struct {
		name     string
		attempts int
		wantSend bool
	}{
		{"最后一次发送失败", 4, true},
		{"最后一次发送后租约过期", 5, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.New()
			if err := q.EnqueueMail(t.Context(), sqlc.EnqueueMailParams{
				ID: id, Kind: "test", Recipient: "test@example.com", Subject: "private subject",
				Body: "private body", ExpiresAt: time.Now().Add(time.Hour),
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(t.Context(), "UPDATE mail_outbox SET attempts=$2, status='processing', lease_id=$3, leased_until=now()-interval '1 second' WHERE id=$1", id, tc.attempts, uuid.New()); err != nil {
				t.Fatal(err)
			}
			sent := false
			service, err := mailoutbox.NewService(pool, func(context.Context, mailoutbox.Message) error {
				sent = true
				return errors.New("供应商不可用")
			}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			if worked, err := service.ProcessOne(t.Context()); err != nil || worked != tc.wantSend || sent != tc.wantSend {
				t.Fatalf("投递次数上限未生效: worked=%v sent=%v err=%v", worked, sent, err)
			}
			var status, recipient, subject, body string
			var attempts int
			var completed bool
			if err := pool.QueryRow(t.Context(), "SELECT status, recipient, subject, body, attempts, completed_at IS NOT NULL FROM mail_outbox WHERE id=$1", id).Scan(&status, &recipient, &subject, &body, &attempts, &completed); err != nil {
				t.Fatal(err)
			}
			if mailoutbox.Status(status) != mailoutbox.StatusFailed || recipient != "" || subject != "" || body != "" || attempts != 5 || !completed {
				t.Fatal("达到投递上限后未正确终止或清除敏感载荷")
			}
			if worked, err := service.ProcessOne(t.Context()); err != nil || worked {
				t.Fatalf("终态邮件不应继续领取: worked=%v err=%v", worked, err)
			}
		})
	}
}
