package mailoutbox_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/example/go-starter-kit/internal/modules/mailoutbox"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRunLogsSafeStageAndPreservesCause(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	output := &cancelWriter{cancel: cancel}
	cause := &pgconn.PgError{Code: "42501", Message: "private database message", Detail: "private mail payload"}
	service, err := mailoutbox.NewService(failedDatabase{err: cause}, func(context.Context, mailoutbox.Message) error {
		t.Fatal("数据库领取失败时不应发送邮件")
		return nil
	}, slog.New(slog.NewJSONHandler(output, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessOne(ctx); worked || !errors.Is(err, cause) {
		t.Fatalf("阶段包装丢失了数据库错误: worked=%v err=%v", worked, err)
	}
	if err := service.Run(ctx, time.Second); err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record); err != nil {
		t.Fatal(err)
	}
	if record["stage"] != "expire" || record["reason_code"] != "database_error" || record["sqlstate"] != "42501" {
		t.Fatalf("队列故障日志缺少阶段或安全分类: %s", output.String())
	}
	for _, secret := range []string{cause.Message, cause.Detail} {
		if strings.Contains(output.String(), secret) {
			t.Fatal("队列故障日志泄露数据库原文")
		}
	}
}

type failedDatabase struct {
	sqlc.DBTX
	err error
}

func (d failedDatabase) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, d.err
}

// cancelWriter 在首条日志写完后停止轮询，测试无需等待计时器或并发读取缓冲区。
type cancelWriter struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (w *cancelWriter) Write(data []byte) (int, error) {
	n, err := w.Buffer.Write(data)
	w.cancel()
	return n, err
}
