package account

import (
	"context"
	"errors"
	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"time"
)

func (s *Service) Register(ctx context.Context, r Request, email string) error {
	email, e := normalizeEmail(email)
	if e != nil {
		return e
	}
	if s.options.FrontendURL == "" {
		return ErrUnavailable
	}
	if e = s.entryLimit(ctx, r, "register", email); e != nil {
		return e
	}
	return db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		u, e := q.CreatePendingUser(ctx, sqlc.CreatePendingUserParams{Email: ptr(email), EmailNormalized: ptr(email)})
		if e != nil {
			return e
		}
		if UserStatus(u.Status) != UserPending {
			return nil
		}
		_, e = s.createChallenge(ctx, q, u, VerifyRegister, email, 24*time.Hour, nil, nil)
		return e
	})
}
func (s *Service) ForgotPassword(ctx context.Context, r Request, email string) error {
	email, e := normalizeEmail(email)
	if e != nil {
		return e
	}
	if s.options.FrontendURL == "" {
		return ErrUnavailable
	}
	if e = s.entryLimit(ctx, r, "forgot", email); e != nil {
		return e
	}
	return db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		u, e := q.FindUserByEmail(ctx, ptr(email))
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		if UserStatus(u.Status) != UserActive || !u.RecoveryEnabled || u.EmailVerifiedAt == nil {
			return nil
		}
		_, e = q.GetPasswordAccount(ctx, u.ID)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		// 邮箱和版本来自同一次读取；并发变更可让挑战失效，由消费时复核。
		_, e = s.createChallenge(ctx, q, u, VerifyResetPassword, text(u.EmailNormalized), 15*time.Minute, nil, nil)
		return e
	})
}
func (s *Service) completeRegistration(ctx context.Context, q *store, u sqlc.User, v sqlc.AuthVerification, hash string) (sqlc.User, error) {
	if UserStatus(u.Status) != UserPending || text(u.EmailNormalized) != v.Email {
		return u, ErrCredentials
	}
	if _, err := q.CreateAccount(ctx, sqlc.CreateAccountParams{UserID: u.ID, ProviderID: CredentialProvider, ProviderAccountID: u.ID.String(), ProviderNamespace: LocalNamespace, PasswordHash: &hash}); err != nil {
		return u, err
	}
	return q.ActivateUser(ctx, u.ID)
}

func (s *Service) completePasswordReset(ctx context.Context, q *store, u sqlc.User, v sqlc.AuthVerification, hash string) error {
	if UserStatus(u.Status) != UserActive || !u.RecoveryEnabled || u.EmailVerifiedAt == nil || text(u.EmailNormalized) != v.Email {
		return ErrCredentials
	}
	a, err := q.GetPasswordAccount(ctx, u.ID)
	if err != nil {
		return credentialLookupError(err)
	}
	if _, err = q.ChangePassword(ctx, sqlc.ChangePasswordParams{ID: a.ID, UserID: u.ID, PasswordHash: &hash}); err != nil {
		return err
	}
	return s.invalidate(ctx, q, u.ID)
}
