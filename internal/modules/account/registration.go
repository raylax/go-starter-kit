package account

import (
	"context"
	"errors"

	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
)

func (s *Service) Register(ctx context.Context, r Request, email string) error {
	email, err := normalizeEmail(email)
	if err != nil {
		return err
	}
	if s.options.FrontendURL == "" {
		return ErrUnavailable
	}
	if err = s.entryLimit(ctx, r, "register", email); err != nil {
		return err
	}
	return db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		user, err := q.CreatePendingUser(ctx, sqlc.CreatePendingUserParams{Email: ptr(email), EmailNormalized: ptr(email)})
		if err != nil {
			return err
		}
		if UserStatus(user.Status) != UserPending {
			return nil
		}
		_, err = s.createChallenge(ctx, q, user, VerifyRegister, email, registrationChallengeTTL, nil, nil)
		return err
	})
}

func (s *Service) ForgotPassword(ctx context.Context, r Request, email string) error {
	email, err := normalizeEmail(email)
	if err != nil {
		return err
	}
	if s.options.FrontendURL == "" {
		return ErrUnavailable
	}
	if err = s.entryLimit(ctx, r, "forgot", email); err != nil {
		return err
	}
	return db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		user, err := q.FindUserByEmail(ctx, ptr(email))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if UserStatus(user.Status) != UserActive || !user.RecoveryEnabled || user.EmailVerifiedAt == nil {
			return nil
		}
		_, err = q.GetPasswordAccount(ctx, user.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		// 邮箱和版本来自同一次读取；并发变更可让挑战失效，由消费时复核。
		_, err = s.createChallenge(ctx, q, user, VerifyResetPassword, text(user.EmailNormalized), passwordResetChallengeTTL, nil, nil)
		return err
	})
}

func (s *Service) completeRegistration(ctx context.Context, q *store, user sqlc.User, verification sqlc.AuthVerification, passwordHash string) (sqlc.User, error) {
	if UserStatus(user.Status) != UserPending || text(user.EmailNormalized) != verification.Email {
		return user, ErrCredentials
	}
	if _, err := q.CreateAccount(ctx, sqlc.CreateAccountParams{
		UserID:            user.ID,
		ProviderID:        CredentialProvider,
		ProviderAccountID: user.ID.String(),
		ProviderNamespace: LocalNamespace,
		PasswordHash:      &passwordHash,
	}); err != nil {
		return user, err
	}
	return q.ActivateUser(ctx, user.ID)
}

func (s *Service) completePasswordReset(ctx context.Context, q *store, user sqlc.User, verification sqlc.AuthVerification, passwordHash string) error {
	if UserStatus(user.Status) != UserActive || !user.RecoveryEnabled || user.EmailVerifiedAt == nil || text(user.EmailNormalized) != verification.Email {
		return ErrCredentials
	}
	passwordAccount, err := q.GetPasswordAccount(ctx, user.ID)
	if err != nil {
		return credentialLookupError(err)
	}
	if _, err = q.ChangePassword(ctx, sqlc.ChangePasswordParams{ID: passwordAccount.ID, UserID: user.ID, PasswordHash: &passwordHash}); err != nil {
		return err
	}
	return s.invalidate(ctx, q, user.ID)
}
