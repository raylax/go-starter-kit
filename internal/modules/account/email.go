package account

import (
	"context"

	"github.com/example/go-starter-kit/internal/authorization"
	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Service) ChangeEmail(ctx context.Context, r Request, email string, reauthID uuid.UUID) (failureErr error) {
	defer s.recordFailureOnReturn(ctx, r, "user.email_change", &failureErr)
	email, err := normalizeEmail(email)
	if err != nil {
		return err
	}
	if s.options.FrontendURL == "" {
		return ErrUnavailable
	}
	if err = s.entryLimit(ctx, r, "email", r.UserID); err != nil {
		return err
	}
	return db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		user, session, err := s.readSelf(ctx, q, r)
		if err != nil {
			return err
		}
		if _, err = s.authorization(ctx, q, r, reauthID, OperationChangeEmail, email, nil); err != nil {
			return err
		}
		verification, err := s.createChallenge(ctx, q, user, VerifyChangeEmail, email, emailChangeChallengeTTL, &session.ID, &reauthID)
		if err != nil {
			return err
		}
		return proofError(q.claimAuthorization(ctx, reauthID, verification.ID), ErrReauthentication)
	})
}

func (s *Service) completeEmailChange(ctx context.Context, q *store, user sqlc.User, verification sqlc.AuthVerification) error {
	if verification.SessionID == nil || verification.ReauthenticationID == nil {
		return ErrCredentials
	}
	req := Request{Subject: authorization.Subject{
		UserID:    user.ID.String(),
		SessionID: verification.SessionID.String(),
	}}
	if _, err := q.GetValidSession(ctx, sqlc.GetValidSessionParams{ID: *verification.SessionID, UserID: user.ID}); err != nil {
		return credentialLookupError(err)
	}
	proof, err := s.authorization(ctx, q, req, *verification.ReauthenticationID, OperationChangeEmail, verification.Email, &verification.ID)
	if err != nil {
		return err
	}
	if err = finishAuthorization(ctx, q, proof); err != nil {
		return err
	}
	if err = q.ReleasePendingEmail(ctx, verification.Email); err != nil {
		return err
	}
	if _, err = q.UpdateUserEmail(ctx, sqlc.UpdateUserEmailParams{
		ID:              user.ID,
		Email:           &verification.Email,
		EmailNormalized: &verification.Email,
	}); err != nil {
		return err
	}
	if err = q.RevokeUserSessions(ctx, user.ID); err != nil {
		return err
	}
	return enqueueSecurityNotification(ctx, q, user, emailChangedNotification)
}
