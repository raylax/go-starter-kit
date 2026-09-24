package account

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/url"
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
	err := q.EnqueueMail(ctx, sqlc.EnqueueMailParams{ID: uuid.New(), Kind: string(purpose), Recipient: email, Subject: "Verify your account", Body: fmt.Sprintf(`<p>请在有效期内完成账户验证。请勿转发此邮件。</p><p><a href="%s">验证账户</a></p>`, html.EscapeString(link.String())), ExpiresAt: v.ExpiresAt})
	return v, err
}
func (s *Service) VerifyChallenge(ctx context.Context, r Request, token string, input Verification) (failureErr error) {
	defer func() {
		failureErr = proofError(failureErr, ErrVerification)
		s.recordFailure(ctx, r, "auth.verify", failureErr)
	}()
	if e := s.limit(ctx, "verify.ip", r.ClientIP, ipRequestLimit, ipRateWindow); e != nil {
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
		if !s.deps.Passwords.Validate(input.NewPassword) {
			return ErrInvalid
		}
		passwordHash, e = s.deps.Passwords.Hash(ctx, input.NewPassword)
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
		var notification string
		switch input.Purpose {
		case VerifyRegister:
			u, e = s.completeRegistration(ctx, q, u, v, passwordHash)
			notification = "您的账户已完成注册，邮箱验证及密码设置成功。您现在可以登录。"
		case VerifyResetPassword:
			e = s.completePasswordReset(ctx, q, u, v, passwordHash)
			notification = "您的账户密码已重置。如非本人操作，请立即联系管理员。"
		case VerifyChangeEmail:
			e = s.completeEmailChange(ctx, q, u, v)
		default:
			e = ErrInvalid
		}
		if e != nil {
			return e
		}
		if e = q.consumeVerification(ctx, v.ID, u.ID); e != nil {
			return e
		}
		if notification != "" {
			if err := q.enqueueSecurityNotification(ctx, u, notification); err != nil {
				return err
			}
		}
		r.UserID = u.ID.String()
		return audit(ctx, q, r, "auth."+string(input.Purpose), AuditSuccess, "user", u.ID.String(), u.ID.String(), "", nil)
	})
}
