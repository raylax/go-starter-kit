package account

import (
	"context"
	"time"

	"github.com/example/go-starter-kit/internal/authorization"
	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Service) ChangeEmail(ctx context.Context, r Request, email string, reauthID uuid.UUID) (failureErr error) {
	defer s.recordFailureOnReturn(ctx, r, "user.email_change", &failureErr)
	email, e := normalizeEmail(email)
	if e != nil {
		return e
	}
	if s.options.FrontendURL == "" {
		return ErrUnavailable
	}
	if e = s.entryLimit(ctx, r, "email", r.UserID); e != nil {
		return e
	}
	return db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		u, session, e := s.readSelf(ctx, q, r)
		if e != nil {
			return e
		}
		if _, e = s.authorization(ctx, q, r, reauthID, OperationChangeEmail, email, nil); e != nil {
			return e
		}
		v, e := s.createChallenge(ctx, q, u, VerifyChangeEmail, email, 5*time.Minute, &session.ID, &reauthID)
		if e != nil {
			return e
		}
		return proofError(q.claimAuthorization(ctx, reauthID, v.ID), ErrReauthentication)
	})
}
func (s *Service) completeEmailChange(ctx context.Context, q *store, u sqlc.User, v sqlc.AuthVerification) error {
	if v.SessionID == nil || v.ReauthenticationID == nil {
		return ErrCredentials
	}
	req := Request{Subject: authorization.Subject{UserID: u.ID.String(), SessionID: v.SessionID.String()}}
	if _, err := q.GetValidSession(ctx, sqlc.GetValidSessionParams{ID: *v.SessionID, UserID: u.ID}); err != nil {
		return credentialLookupError(err)
	}
	proof, err := s.authorization(ctx, q, req, *v.ReauthenticationID, OperationChangeEmail, v.Email, &v.ID)
	if err != nil {
		return err
	}
	if err = finishAuthorization(ctx, q, proof); err != nil {
		return err
	}
	if _, err = q.UpdateUserEmail(ctx, sqlc.UpdateUserEmailParams{ID: u.ID, Email: &v.Email, EmailNormalized: &v.Email}); err != nil {
		return err
	}
	if err = q.RevokeUserSessions(ctx, u.ID); err != nil {
		return err
	}
	return q.enqueueSecurityNotification(ctx, u, "账户联系邮箱已修改。如果不是本人操作，请联系管理员。")
}
