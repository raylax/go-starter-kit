package account

import (
	"context"
	"errors"
	"fmt"
	"github.com/example/go-starter-kit/internal/authorization"
	"github.com/example/go-starter-kit/internal/db"
	"html"
	"net/url"
	"time"

	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Service) createChallenge(ctx context.Context, q *store, u sqlc.User, purpose VerificationPurpose, email string, ttl time.Duration, session, reauth *uuid.UUID) (sqlc.AuthVerification, error) {
	token, hash := identity.NewToken(ChallengePrefix)
	v, e := q.CreateVerification(ctx, sqlc.CreateVerificationParams{UserID: u.ID, Purpose: string(purpose), TokenHash: hash, Email: email, AuthVersion: u.AuthVersion, SessionID: session, ReauthenticationID: reauth, TtlSeconds: int64(ttl.Seconds())})
	if e != nil {
		return v, e
	}
	link, e := url.Parse(s.options.FrontendURL)
	if e != nil {
		return v, ErrUnavailable
	}
	link.Path = "/auth/verify"
	link.RawQuery = ""
	link.Fragment = url.Values{"token": {token}, "purpose": {string(purpose)}}.Encode()
	err := q.EnqueueMail(ctx, sqlc.EnqueueMailParams{ID: uuid.Must(uuid.NewV7()), Kind: string(purpose), Recipient: email, Subject: "Verify your account", Body: fmt.Sprintf(`<p>请在有效期内完成账户验证。请勿转发此邮件。</p><p><a href="%s">验证账户</a></p>`, html.EscapeString(link.String())), ExpiresAt: v.ExpiresAt})
	return v, err
}
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
		u, e = q.LockUser(ctx, u.ID)
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
		_, e = s.createChallenge(ctx, q, u, VerifyResetPassword, email, 15*time.Minute, nil, nil)
		return e
	})
}
func (s *Service) VerifyChallenge(ctx context.Context, r Request, token string, input Verification) (failureErr error) {
	defer func() {
		failureErr = proofError(failureErr, ErrVerification)
		s.recordFailure(ctx, r, "auth.verify", failureErr)
	}()
	if e := s.limit(ctx, "verify.ip", r.ClientIP, 60, time.Minute); e != nil {
		return e
	}
	hash, e := identity.TokenDigest(token, ChallengePrefix)
	if e != nil {
		return ErrCredentials
	}
	v, e := s.queries.FindVerification(ctx, hash)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrCredentials
	}
	if e != nil {
		return e
	}
	if VerificationPurpose(v.Purpose) != input.Purpose {
		return ErrCredentials
	}
	var passwordHash string
	switch input.Purpose {
	case VerifyRegister, VerifyResetPassword:
		if !s.deps.ValidPassword(input.NewPassword) {
			return ErrInvalid
		}
		passwordHash, e = s.deps.Hash(ctx, input.NewPassword)
		if e != nil {
			return e
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
		u, e := q.LockUser(ctx, v.UserID)
		if e != nil {
			return e
		}
		if u.AuthVersion != v.AuthVersion || UserStatus(u.Status) == UserDisabled {
			return ErrCredentials
		}
		if input.Purpose == VerifyRegister {
			if UserStatus(u.Status) != UserPending || text(u.EmailNormalized) != v.Email {
				return ErrCredentials
			}
			_, e = q.CreateAccount(ctx, sqlc.CreateAccountParams{UserID: u.ID, ProviderID: CredentialProvider, ProviderAccountID: u.ID.String(), ProviderNamespace: LocalNamespace, PasswordHash: &passwordHash})
			if e != nil {
				return e
			}
			u, e = q.ActivateUser(ctx, u.ID)
			if e != nil {
				return e
			}
		} else if input.Purpose == VerifyResetPassword {
			if UserStatus(u.Status) != UserActive || !u.RecoveryEnabled || u.EmailVerifiedAt == nil || text(u.EmailNormalized) != v.Email {
				return ErrCredentials
			}
			a, e := q.GetPasswordAccount(ctx, u.ID)
			if errors.Is(e, pgx.ErrNoRows) {
				return ErrCredentials
			}
			if e != nil {
				return e
			}
			if _, e = q.ChangePassword(ctx, sqlc.ChangePasswordParams{ID: a.ID, UserID: u.ID, PasswordHash: &passwordHash}); e != nil {
				return e
			}
			if e = s.invalidate(ctx, q, u.ID); e != nil {
				return e
			}
		} else {
			if v.SessionID == nil || v.ReauthenticationID == nil {
				return ErrCredentials
			}
			req := Request{Subject: authorization.Subject{UserID: u.ID.String(), SessionID: v.SessionID.String()}}
			if _, e = q.GetValidSession(ctx, sqlc.GetValidSessionParams{ID: *v.SessionID, UserID: u.ID}); e != nil {
				return credentialLookupError(e)
			}
			proof, e := s.authorization(ctx, q, req, *v.ReauthenticationID, OperationChangeEmail, v.Email, &v.ID)
			if e != nil {
				return e
			}
			if e = finishAuthorization(ctx, q, proof); e != nil {
				return e
			}
			if _, e = q.UpdateUserEmail(ctx, sqlc.UpdateUserEmailParams{ID: u.ID, Email: &v.Email, EmailNormalized: &v.Email}); e != nil {
				return e
			}
			if e = q.RevokeUserSessions(ctx, u.ID); e != nil {
				return e
			}
			if err := q.enqueueSecurityNotification(ctx, u, "账户联系邮箱已修改。如果不是本人操作，请联系管理员。"); err != nil {
				return err
			}
		}
		count, e := q.ConsumeVerification(ctx, sqlc.ConsumeVerificationParams{ID: v.ID, UserID: u.ID})
		if e != nil {
			return e
		}
		if count != 1 {
			return ErrCredentials
		}
		if input.Purpose != VerifyChangeEmail {
			if err := q.enqueueSecurityNotification(ctx, u, "账户验证或密码恢复已完成，请重新登录。"); err != nil {
				return err
			}
		}
		r.UserID = u.ID.String()
		return audit(ctx, q, r, "auth."+string(input.Purpose), AuditSuccess, "user", u.ID.String(), u.ID.String(), "", nil)
	})
}

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
		u, session, e := s.lockSelf(ctx, q, r)
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
		count, e := q.ClaimAuthorization(ctx, sqlc.ClaimAuthorizationParams{ID: reauthID, ClaimedBy: &v.ID})
		if e != nil {
			return e
		}
		if count != 1 {
			return ErrCredentials
		}
		return nil
	})
}
