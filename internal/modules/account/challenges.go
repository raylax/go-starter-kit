package account

import (
	"context"
	"errors"
	"time"

	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	registrationChallengeTTL  = 24 * time.Hour
	passwordResetChallengeTTL = 15 * time.Minute
	emailChangeChallengeTTL   = 5 * time.Minute
)

func (s *Service) createChallenge(ctx context.Context, q *store, user sqlc.User, purpose VerificationPurpose, email string, ttl time.Duration, session, reauth *uuid.UUID) (sqlc.AuthVerification, error) {
	token, hash := identity.NewToken(ChallengePrefix)
	verification, err := q.CreateVerification(ctx, sqlc.CreateVerificationParams{
		UserID:             user.ID,
		Purpose:            string(purpose),
		TokenHash:          hash,
		Email:              email,
		AuthVersion:        user.AuthVersion,
		SessionID:          session,
		ReauthenticationID: reauth,
		TtlSeconds:         int64(ttl.Seconds()),
	})
	if err != nil {
		return verification, err
	}
	err = s.enqueueVerificationMail(ctx, q, verification, token)
	return verification, err
}

func (s *Service) VerifyChallenge(ctx context.Context, r Request, token string, input Verification) (failureErr error) {
	defer func() {
		failureErr = proofError(failureErr, ErrVerification)
		s.recordFailure(ctx, r, "auth.verify", failureErr)
	}()
	if err := s.limit(ctx, "verify.ip", r.ClientIP, ipRequestLimit, ipRateWindow); err != nil {
		return err
	}
	hash, err := identity.TokenDigest(token, ChallengePrefix)
	if err != nil {
		return ErrCredentials
	}
	verification, err := s.queries.FindVerification(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCredentials
	}
	if err != nil {
		return err
	}
	if VerificationPurpose(verification.Purpose) != input.Purpose {
		return ErrCredentials
	}
	var passwordHash string
	switch input.Purpose {
	case VerifyRegister, VerifyResetPassword:
		if !s.deps.Passwords.Validate(input.NewPassword) {
			return ErrInvalid
		}
		passwordHash, err = s.deps.Passwords.Hash(ctx, input.NewPassword)
		if err != nil {
			return err
		}
	case VerifyChangeEmail:
		if input.NewPassword != "" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		user, err := q.LockUser(ctx, verification.UserID)
		if err != nil {
			return err
		}
		if user.AuthVersion != verification.AuthVersion || UserStatus(user.Status) == UserDisabled {
			return ErrCredentials
		}
		var notification string
		switch input.Purpose {
		case VerifyRegister:
			user, err = s.completeRegistration(ctx, q, user, verification, passwordHash)
			notification = registrationCompletedNotification
		case VerifyResetPassword:
			err = s.completePasswordReset(ctx, q, user, verification, passwordHash)
			notification = passwordResetNotification
		case VerifyChangeEmail:
			err = s.completeEmailChange(ctx, q, user, verification)
		default:
			err = ErrInvalid
		}
		if err != nil {
			return err
		}
		if err = q.consumeVerification(ctx, verification.ID, user.ID); err != nil {
			return err
		}
		if notification != "" {
			if err := enqueueSecurityNotification(ctx, q, user, notification); err != nil {
				return err
			}
		}
		r.UserID = user.ID.String()
		return audit(ctx, q, r, auditEvent{
			Action:       "auth." + string(input.Purpose),
			Outcome:      AuditSuccess,
			ResourceType: "user",
			ResourceID:   user.ID.String(),
			ScopeSubject: user.ID.String(),
		})
	})
}
