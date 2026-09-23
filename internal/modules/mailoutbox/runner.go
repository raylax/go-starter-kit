package mailoutbox

import (
	"context"
	"fmt"
	"time"
)

// Run 串行消费，多个 Worker 可通过 SKIP LOCKED 并行处理不同邮件。
func (s *Service) Run(ctx context.Context, poll time.Duration) error {
	if poll <= 0 {
		return fmt.Errorf("邮件轮询间隔必须为正")
	}
	for ctx.Err() == nil {
		worked, err := s.ProcessOne(ctx)
		if ctx.Err() != nil {
			break
		}
		if err != nil {
			s.logger.ErrorContext(ctx, "邮件队列处理失败")
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
