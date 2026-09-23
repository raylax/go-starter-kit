package mail

import (
	"context"
	"log/slog"
)

// LogSender 仅记录发送调用，不连接邮件服务或输出邮件内容。
type LogSender struct{ logger *slog.Logger }

func NewLogSender(logger *slog.Logger) *LogSender { return &LogSender{logger: logger} }

func (s *LogSender) Send(ctx context.Context, message Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.logger.InfoContext(ctx, "邮件发送 mock", "provider", "log", "message_id", message.ID, "kind", message.Kind, "delivered", false)
	return nil
}

var _ Sender = (*LogSender)(nil)
