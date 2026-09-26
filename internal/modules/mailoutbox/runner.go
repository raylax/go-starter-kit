package mailoutbox

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/example/go-starter-kit/internal/apperror"
)

// Run 串行消费，多个 Worker 可通过 SKIP LOCKED 并行处理不同邮件。
func (s *Service) Run(ctx context.Context, poll time.Duration) error {
	if poll <= 0 {
		return errors.New("邮件轮询间隔必须为正")
	}
	for ctx.Err() == nil {
		worked, err := s.ProcessOne(ctx)
		// 停机导致的查询取消无需报错，但取消后完成状态的落库失败仍须记录。
		if err != nil && (worked || ctx.Err() == nil || !errors.Is(err, ctx.Err())) {
			attrs := apperror.DiagnosticAttrs(err)
			var failure *stageError
			if errors.As(err, &failure) {
				attrs = failure.attrs()
			}
			s.logger.LogAttrs(ctx, slog.LevelError, "邮件队列处理失败", attrs...)
		}
		if ctx.Err() != nil {
			break
		}
		if worked && err == nil {
			continue
		}
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	return nil
}
