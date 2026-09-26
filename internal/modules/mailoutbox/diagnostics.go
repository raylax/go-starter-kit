package mailoutbox

import (
	"log/slog"

	"github.com/example/go-starter-kit/internal/apperror"
	"github.com/google/uuid"
)

// stageError 保留内部错误链；日志只使用固定阶段、记录引用和安全分类。
type stageError struct {
	stage     string
	messageID uuid.UUID
	attempt   int32
	cause     error
}

func (e *stageError) Error() string { return "邮件队列在 " + e.stage + " 阶段失败" }
func (e *stageError) Unwrap() error { return e.cause }

func (e *stageError) attrs() []slog.Attr {
	attrs := []slog.Attr{slog.String("stage", e.stage)}
	if e.messageID != uuid.Nil {
		attrs = append(attrs, slog.String("message_id", e.messageID.String()), slog.Int("attempt", int(e.attempt)))
	}
	return append(attrs, apperror.DiagnosticAttrs(e.cause)...)
}
