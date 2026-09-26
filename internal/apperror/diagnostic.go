package apperror

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
)

// DiagnosticAttrs 从错误链提取安全分类，不记录错误原文、数据库明细或供应商响应。
// SQLState 使用能力接口识别，避免公共错误包依赖具体数据库驱动。
func DiagnosticAttrs(err error) []slog.Attr {
	if err == nil {
		return nil
	}
	reason := "internal_error"
	var databaseError interface{ SQLState() string }
	var networkError net.Error
	var business *Error
	var sqlState string
	switch {
	case errors.Is(err, context.Canceled):
		reason = "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		reason = "deadline_exceeded"
	case errors.As(err, &databaseError):
		reason = "database_error"
		if code := databaseError.SQLState(); validSQLState(code) {
			sqlState = code
		}
	case errors.As(err, &networkError):
		reason = "network_error"
		if networkError.Timeout() {
			reason = "network_timeout"
		}
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		reason = "connection_closed"
	case errors.As(err, &business):
		switch business.Kind() {
		case NotFound:
			reason = "not_found"
		case Conflict:
			reason = "conflict"
		case Invalid:
			reason = "invalid_input"
		case Unauthenticated:
			reason = "unauthenticated"
		case Unavailable:
			reason = "dependency_unavailable"
		case Forbidden:
			reason = "forbidden"
		case RateLimited:
			reason = "rate_limited"
		}
	}
	attrs := []slog.Attr{slog.String("reason_code", reason)}
	if sqlState != "" {
		attrs = append(attrs, slog.String("sqlstate", sqlState))
	}
	return attrs
}

func validSQLState(code string) bool {
	const sqlStateBytes = 5
	if len(code) != sqlStateBytes {
		return false
	}
	for _, character := range code {
		if (character < '0' || character > '9') && (character < 'A' || character > 'Z') {
			return false
		}
	}
	return true
}
