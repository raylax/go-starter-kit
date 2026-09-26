package apperror_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"

	"github.com/example/go-starter-kit/internal/apperror"
)

type diagnosticDatabaseError struct{ code string }

func (e diagnosticDatabaseError) Error() string    { return "private-error-payload" }
func (e diagnosticDatabaseError) SQLState() string { return e.code }

func TestDiagnosticsClassifyCausesWithoutExposingPayload(t *testing.T) {
	for _, test := range []struct {
		name, reason, sqlState string
		err                    error
	}{
		{name: "数据库权限", reason: "database_error", sqlState: "42501", err: diagnosticDatabaseError{"42501"}},
		{name: "无效数据库分类", reason: "database_error", err: diagnosticDatabaseError{"private-error-payload"}},
		{name: "包装超时", reason: "deadline_exceeded", err: fmt.Errorf("private-error-payload: %w", context.DeadlineExceeded)},
		{name: "取消", reason: "canceled", err: context.Canceled},
		{name: "网络故障", reason: "network_error", err: &net.OpError{Op: "dial", Err: errors.New("private-error-payload")}},
		{name: "网络超时", reason: "network_timeout", err: &net.DNSError{Err: "private-error-payload", IsTimeout: true}},
		{name: "连接中断", reason: "connection_closed", err: io.ErrUnexpectedEOF},
		{name: "业务分类", reason: "dependency_unavailable", err: apperror.New(apperror.Unavailable, "private-error-payload")},
		{name: "保留底层分类", reason: "database_error", sqlState: "42P01", err: apperror.Wrap(apperror.New(apperror.Unavailable, "private-error-payload"), diagnosticDatabaseError{"42P01"})},
		{name: "未知错误", reason: "internal_error", err: errors.New("private-error-payload")},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			logger.LogAttrs(t.Context(), slog.LevelError, "诊断", apperror.DiagnosticAttrs(test.err)...)
			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record["reason_code"] != test.reason || (test.sqlState != "" && record["sqlstate"] != test.sqlState) {
				t.Fatalf("错误分类不正确：%v", record)
			}
			if test.sqlState == "" && record["sqlstate"] != nil {
				t.Fatal("无效 SQLSTATE 被写入日志")
			}
			if strings.Contains(output.String(), "private-error-payload") {
				t.Fatal("诊断日志泄露原始错误")
			}
		})
	}
	if len(apperror.DiagnosticAttrs(nil)) != 0 {
		t.Fatal("成功结果不应产生诊断属性")
	}
}
