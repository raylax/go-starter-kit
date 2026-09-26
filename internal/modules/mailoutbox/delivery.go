package mailoutbox

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	maxDeliveryAttempts  = 5
	leaseDuration        = time.Minute
	expiredMailBatchSize = 100
	databaseTimeout      = 5 * time.Second
	sendTimeout          = 10 * time.Second
	leaseFinishReserve   = 5 * time.Second
	retryBaseSeconds     = 15
	retryJitterSeconds   = 10
)

// ProcessOne 原子领取一封邮件，在数据库事务外发送。租约标识防止过期消费者覆盖新状态。
func (s *Service) ProcessOne(ctx context.Context) (bool, error) {
	queryCtx, cancel := context.WithTimeout(ctx, databaseTimeout)
	defer cancel()
	if err := s.queries.ExpireMail(queryCtx, sqlc.ExpireMailParams{MaxAttempts: maxDeliveryAttempts, BatchSize: expiredMailBatchSize}); err != nil {
		return false, &stageError{stage: "expire", cause: err}
	}
	lease, err := uuid.NewRandom()
	if err != nil {
		return false, &stageError{stage: "create_lease", cause: err}
	}
	row, err := s.queries.ClaimMail(queryCtx, sqlc.ClaimMailParams{LeaseID: &lease, MaxAttempts: maxDeliveryAttempts, LeaseSeconds: int64(leaseDuration / time.Second)})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, &stageError{stage: "claim", cause: err}
	}
	if Status(row.Status) != StatusProcessing || row.LeaseID == nil || *row.LeaseID != lease || row.LeasedUntil == nil {
		return true, &stageError{stage: "validate_claim", messageID: row.ID, attempt: row.Attempts, cause: errors.New("邮件领取状态无效")}
	}
	sendErr := s.deliver(ctx, row)
	return true, s.finishDelivery(ctx, row, lease, sendErr)
}

// deliver 限定外部发送时间，始终在数据库事务外执行。
func (s *Service) deliver(ctx context.Context, row sqlc.MailOutbox) error {
	// 单次发送期限早于租约到期，并且不超过邮件本身的有效期。
	deadline := time.Now().Add(sendTimeout)
	if row.ExpiresAt.Before(deadline) {
		deadline = row.ExpiresAt
	}
	if limit := row.LeasedUntil.Add(-leaseFinishReserve); limit.Before(deadline) {
		deadline = limit
	}
	sendCtx, stop := context.WithDeadline(ctx, deadline)
	sendErr := sendCtx.Err()
	if sendErr == nil {
		sendErr = s.send(sendCtx, Message{ID: row.ID.String(), Kind: row.Kind, To: row.Recipient, Subject: row.Subject, HTML: row.Body})
	}
	stop()
	return sendErr
}

// finishDelivery 在停机取消后仍有机会落库；租约过期时由其他消费者恢复。
func (s *Service) finishDelivery(ctx context.Context, row sqlc.MailOutbox, lease uuid.UUID, sendErr error) error {
	// 停机时仍尝试完成本次状态更新，失败则依靠租约过期恢复。
	finishCtx, finish := context.WithTimeout(context.WithoutCancel(ctx), databaseTimeout)
	defer finish()
	var count int64
	var err error
	stage := "complete"
	if sendErr == nil {
		count, err = s.queries.CompleteMail(finishCtx, sqlc.CompleteMailParams{ID: row.ID, LeaseID: &lease})
	} else {
		stage = "retry"
		delay := retryDelaySeconds(row.Attempts)
		count, err = s.queries.RetryMail(finishCtx, sqlc.RetryMailParams{ID: row.ID, LeaseID: &lease, DelaySeconds: delay, MaxAttempts: maxDeliveryAttempts})
		failure := &stageError{stage: "send", messageID: row.ID, attempt: row.Attempts, cause: sendErr}
		s.logger.LogAttrs(ctx, slog.LevelWarn, "邮件投递失败", failure.attrs()...)
	}
	if err != nil {
		return &stageError{stage: stage, messageID: row.ID, attempt: row.Attempts, cause: err}
	}
	if count != 1 {
		return &stageError{stage: "confirm_lease", messageID: row.ID, attempt: row.Attempts, cause: errors.New("邮件领取租约已失效")}
	}
	return nil
}

// retryDelaySeconds 按领取次数退避，抖动避免故障恢复时集中重试。
func retryDelaySeconds(attempt int32) int64 {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > maxDeliveryAttempts {
		attempt = maxDeliveryAttempts
	}
	return int64(retryBaseSeconds*(1<<uint(attempt-1))) + int64(rand.IntN(retryJitterSeconds))
}
