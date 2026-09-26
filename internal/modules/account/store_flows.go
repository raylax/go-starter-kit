package account

import (
	"context"
	"time"

	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/example/go-starter-kit/internal/validation"
)

// transitionResult 统一原子状态转换的行数判定，零行表示证明已失效或被消费。
func transitionResult(count int64, err error) error {
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrCredentials
	}
	return nil
}
func (q *store) claimPendingFlow(ctx context.Context, id string) error {
	return transitionResult(q.queries.ClaimFlow(ctx, id))
}
func (q *store) claimAuthorization(ctx context.Context, id, claim string) error {
	return transitionResult(q.queries.ClaimAuthorization(ctx, sqlc.ClaimAuthorizationParams{ID: id, ClaimedBy: &claim}))
}
func (q *store) consumeFlow(ctx context.Context, id string, expected FlowStatus) error {
	switch expected {
	case FlowProcessing, FlowVerified, FlowAuthorized, FlowClaimed:
		return transitionResult(q.queries.FinishFlow(ctx, sqlc.FinishFlowParams{ID: id, Status: string(expected)}))
	default:
		return ErrCredentials
	}
}
func (q *store) verifyFlow(ctx context.Context, id string, status FlowStatus, verified VerifiedIdentity) error {
	if !status.Verifiable() || !validBytes(verified.Namespace, maxProviderNamespaceBytes) || verified.Namespace == LocalNamespace || !validBytes(verified.Subject, maxProviderSubjectBytes) || !validation.TextWithin(verified.Name, maxDisplayNameRunes) {
		return ErrInvalid
	}
	// 记录本应用完成证明校验的时间；GitHub OAuth 不提供用户重新输入凭据的时间。
	verifiedAt := time.Now()
	return transitionResult(q.queries.VerifyFlow(ctx, sqlc.VerifyFlowParams{
		ID:                id,
		Status:            string(status),
		VerifiedNamespace: verified.Namespace,
		VerifiedSubject:   verified.Subject,
		VerifiedName:      verified.Name,
		AuthenticatedAt:   &verifiedAt,
	}))
}
func (q *store) consumeVerification(ctx context.Context, id, user string) error {
	return transitionResult(q.queries.ConsumeVerification(ctx, sqlc.ConsumeVerificationParams{ID: id, UserID: user}))
}

func (q *store) FailFlow(ctx context.Context, id string) error {
	return q.queries.FailFlow(ctx, id)
}
