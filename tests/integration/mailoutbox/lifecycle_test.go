//go:build integration

package mailoutbox_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/example/go-starter-kit/internal/modules/mailoutbox"
	"github.com/example/go-starter-kit/tests/integration/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const asyncTestTimeout = 10 * time.Second

func TestProcessOneExcludesConcurrentConsumer(t *testing.T) {
	pool, _ := testutil.Database(t)
	id := enqueueMessage(t, pool, time.Now().Add(time.Hour))
	ctx, cancel := context.WithTimeout(t.Context(), asyncTestTimeout)
	defer cancel()
	entered := make(chan mailoutbox.Message, 1)
	release := make(chan struct{})
	first := newConsumer(t, pool, func(ctx context.Context, message mailoutbox.Message) error {
		entered <- message
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}, nil)
	done := processAsync(ctx, first)
	if message := await(t, entered); message.ID != id.String() {
		t.Fatal("消费者未收到目标邮件")
	}
	second := newConsumer(t, pool, func(context.Context, mailoutbox.Message) error {
		t.Error("租约有效时第二个消费者不应发送同一封邮件")
		return nil
	}, nil)
	if worked, err := second.ProcessOne(ctx); worked || err != nil {
		t.Fatalf("第二个消费者重复领取邮件: worked=%v err=%v", worked, err)
	}
	close(release)
	if result := await(t, done); !result.worked || result.err != nil {
		t.Fatalf("首个消费者未完成投递: %+v", result)
	}
	assertMessageState(t, pool, id, mailoutbox.StatusSent, 1, true)
}

func TestProcessOnePersistsRetryAfterCancellation(t *testing.T) {
	pool, _ := testutil.Database(t)
	id := enqueueMessage(t, pool, time.Now().Add(time.Hour))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{}, 1)
	var logs bytes.Buffer
	service := newConsumer(t, pool, func(ctx context.Context, _ mailoutbox.Message) error {
		entered <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}, slog.New(slog.NewJSONHandler(&logs, nil)))
	done := processAsync(ctx, service)
	await(t, entered)
	cancel()
	if result := await(t, done); !result.worked || result.err != nil {
		t.Fatalf("取消发送后未完成独立上下文中的重试落库: %+v", result)
	}
	assertMessageState(t, pool, id, mailoutbox.StatusPending, 1, false)
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &record); err != nil {
		t.Fatal(err)
	}
	if record["stage"] != "send" || record["message_id"] != id.String() || record["attempt"] != float64(1) || record["reason_code"] != "canceled" {
		t.Fatalf("发送取消日志缺少安全诊断属性: %s", logs.String())
	}
}

func TestRunLogsWriteTimeoutAfterParentDeadline(t *testing.T) {
	pool, _ := testutil.Database(t)
	id := enqueueMessage(t, pool, time.Now().Add(time.Hour))
	// 数据库和邮件准备完成后才启动期限，给领取操作留出足够时间。
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	database := &failNextExec{DBTX: pool}
	entered := make(chan struct{}, 1)
	var logs bytes.Buffer
	service := newConsumer(t, database, func(sendCtx context.Context, _ mailoutbox.Message) error {
		entered <- struct{}{}
		<-sendCtx.Done()
		// 完成上下文独立于父上下文，但可能同样以 DeadlineExceeded 失败。
		database.next = context.DeadlineExceeded
		return sendCtx.Err()
	}, slog.New(slog.NewJSONHandler(&logs, nil)))
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx, time.Second) }()
	await(t, entered)
	if ctx.Err() != nil {
		t.Fatal("父上下文应在进入发送后才超时")
	}
	if err := await(t, done); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatal("未触发父上下文超时")
	}
	decoder := json.NewDecoder(&logs)
	found := false
	for {
		var record map[string]any
		if err := decoder.Decode(&record); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if record["stage"] == "retry" && record["level"] == "ERROR" && record["reason_code"] == "deadline_exceeded" && record["message_id"] == id.String() && record["attempt"] == float64(1) {
			found = true
		}
	}
	if !found {
		t.Fatal("父上下文超时掩盖了独立状态回写的超时诊断")
	}
}

func TestProcessOneRecoversAfterCompletionWriteFailure(t *testing.T) {
	pool, _ := testutil.Database(t)
	id := enqueueMessage(t, pool, time.Now().Add(time.Hour))
	writeErr := &pgconn.PgError{Code: "08006", Message: "private database detail"}
	database := &failNextExec{DBTX: pool}
	calls := 0
	service := newConsumer(t, database, func(_ context.Context, message mailoutbox.Message) error {
		calls++
		if message.ID != id.String() {
			t.Error("恢复投递改变了供应商幂等键")
		}
		if calls == 1 {
			// 在供应商接受后让下一次状态写入失败，模拟进程与数据库失联。
			database.next = writeErr
		}
		return nil
	}, nil)
	if worked, err := service.ProcessOne(t.Context()); !worked || !errors.Is(err, writeErr) {
		t.Fatalf("完成回写未保留错误链: worked=%v err=%v", worked, err)
	}
	var status string
	var leased bool
	if err := pool.QueryRow(t.Context(), "SELECT status, lease_id IS NOT NULL FROM mail_outbox WHERE id=$1", id).Scan(&status, &leased); err != nil {
		t.Fatal(err)
	}
	if mailoutbox.Status(status) != mailoutbox.StatusProcessing || !leased {
		t.Fatal("回写失败后应保留租约，等待到期恢复")
	}
	if worked, err := service.ProcessOne(t.Context()); worked || err != nil || calls != 1 {
		t.Fatalf("租约未到期就重复发送: worked=%v calls=%d err=%v", worked, calls, err)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE mail_outbox SET leased_until=now()-interval '1 second' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessOne(t.Context()); !worked || err != nil || calls != 2 {
		t.Fatalf("租约到期后未恢复投递: worked=%v calls=%d err=%v", worked, calls, err)
	}
	assertMessageState(t, pool, id, mailoutbox.StatusSent, 2, true)
}

func TestProcessOneRejectsStaleConsumerCompletion(t *testing.T) {
	pool, _ := testutil.Database(t)
	id := enqueueMessage(t, pool, time.Now().Add(time.Hour))
	ctx, cancel := context.WithTimeout(t.Context(), asyncTestTimeout)
	defer cancel()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	first := newConsumer(t, pool, func(ctx context.Context, _ mailoutbox.Message) error {
		entered <- struct{}{}
		select {
		case <-release:
			return errors.New("private provider failure")
		case <-ctx.Done():
			return ctx.Err()
		}
	}, nil)
	done := processAsync(ctx, first)
	await(t, entered)
	if _, err := pool.Exec(ctx, "UPDATE mail_outbox SET leased_until=now()-interval '1 second' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	second := newConsumer(t, pool, func(_ context.Context, message mailoutbox.Message) error {
		if message.ID != id.String() {
			t.Error("租约恢复改变了供应商幂等键")
		}
		return nil
	}, nil)
	if worked, err := second.ProcessOne(ctx); !worked || err != nil {
		t.Fatalf("新消费者未接管过期租约: worked=%v err=%v", worked, err)
	}
	close(release)
	if result := await(t, done); !result.worked || result.err == nil {
		t.Fatalf("旧消费者未识别租约失效: %+v", result)
	}
	assertMessageState(t, pool, id, mailoutbox.StatusSent, 2, true)
}

func TestProcessOneRespectsMessageDeadline(t *testing.T) {
	pool, _ := testutil.Database(t)
	expiresAt := time.Now().Add(2 * time.Second).Truncate(time.Microsecond)
	id := enqueueMessage(t, pool, expiresAt)
	called := false
	service := newConsumer(t, pool, func(ctx context.Context, _ mailoutbox.Message) error {
		called = true
		deadline, ok := ctx.Deadline()
		if !ok || !deadline.Equal(expiresAt) {
			t.Error("发送期限应受邮件有效期限制")
		}
		<-ctx.Done()
		return ctx.Err()
	}, nil)
	if worked, err := service.ProcessOne(t.Context()); !worked || err != nil || !called {
		t.Fatalf("邮件到期后未结束发送并落库: worked=%v called=%v err=%v", worked, called, err)
	}
	assertMessageState(t, pool, id, mailoutbox.StatusFailed, 1, true)
}

func enqueueMessage(t *testing.T, pool *pgxpool.Pool, expiresAt time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := sqlc.New(pool).EnqueueMail(t.Context(), sqlc.EnqueueMailParams{
		ID: id, Kind: "test", Recipient: "test@example.com", Subject: "private subject", Body: "private body", ExpiresAt: expiresAt,
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func newConsumer(t *testing.T, database sqlc.DBTX, send mailoutbox.SendFunc, logger *slog.Logger) *mailoutbox.Service {
	t.Helper()
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	service, err := mailoutbox.NewService(database, send, logger)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func assertMessageState(t *testing.T, pool *pgxpool.Pool, id uuid.UUID, wantStatus mailoutbox.Status, wantAttempts int, cleared bool) {
	t.Helper()
	var status, recipient, subject, body string
	var attempts int
	var leased bool
	if err := pool.QueryRow(t.Context(), "SELECT status, attempts, lease_id IS NOT NULL, recipient, subject, body FROM mail_outbox WHERE id=$1", id).Scan(&status, &attempts, &leased, &recipient, &subject, &body); err != nil {
		t.Fatal(err)
	}
	if mailoutbox.Status(status) != wantStatus || attempts != wantAttempts || leased {
		t.Fatalf("投递状态不符合预期: status=%s attempts=%d leased=%v", status, attempts, leased)
	}
	if cleared && (recipient != "" || subject != "" || body != "") {
		t.Fatal("投递终态未清除敏感载荷")
	}
	if !cleared && (recipient == "" || subject == "" || body == "") {
		t.Fatal("可重试邮件不应清除载荷")
	}
}

type processResult struct {
	worked bool
	err    error
}

func processAsync(ctx context.Context, service *mailoutbox.Service) <-chan processResult {
	done := make(chan processResult, 1)
	go func() {
		worked, err := service.ProcessOne(ctx)
		done <- processResult{worked: worked, err: err}
	}()
	return done
}

func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(asyncTestTimeout):
		t.Fatal("异步操作未在期限内完成")
		var zero T
		return zero
	}
}

// failNextExec 通过现有数据库依赖注入单次故障，其余操作仍访问真实 PostgreSQL。
type failNextExec struct {
	sqlc.DBTX
	next error
}

func (d *failNextExec) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	if d.next != nil {
		err := d.next
		d.next = nil
		return pgconn.CommandTag{}, err
	}
	return d.DBTX.Exec(ctx, sql, arguments...)
}

func TestSendFailureLogsDoNotExposeProviderResponse(t *testing.T) {
	pool, _ := testutil.Database(t)
	id := enqueueMessage(t, pool, time.Now().Add(time.Hour))
	var logs bytes.Buffer
	service := newConsumer(t, pool, func(context.Context, mailoutbox.Message) error {
		return errors.New("private provider response containing test@example.com and private body")
	}, slog.New(slog.NewJSONHandler(&logs, nil)))
	if worked, err := service.ProcessOne(t.Context()); !worked || err != nil {
		t.Fatalf("供应商失败未进入重试: worked=%v err=%v", worked, err)
	}
	for _, secret := range []string{"private provider response", "test@example.com", "private subject", "private body"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("邮件日志泄露敏感内容")
		}
	}
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &record); err != nil {
		t.Fatal(err)
	}
	if record["stage"] != "send" || record["message_id"] != id.String() || record["reason_code"] == "" || record["reason_code"] == nil {
		t.Fatal("供应商失败日志缺少安全诊断属性")
	}
}
