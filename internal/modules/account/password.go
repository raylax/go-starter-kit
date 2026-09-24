package account

import (
	"context"
	"errors"

	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Service) SetPassword(ctx context.Context, r Request, currentPassword, newPassword string, reauthID uuid.UUID) (failureErr error) {
	defer s.recordFailureOnReturn(ctx, r, "account.password_change", &failureErr)
	if !s.deps.Passwords.Validate(newPassword) {
		return ErrInvalid
	}
	if e := s.entryLimit(ctx, r, "password", r.UserID); e != nil {
		return e
	}
	before, _, e := s.readSelf(ctx, s.queries, r)
	if e != nil {
		return db.MapError(e, storageErrors)
	}
	a, ae := s.queries.GetPasswordAccount(ctx, before.ID)
	if ae != nil && !errors.Is(ae, pgx.ErrNoRows) {
		return ae
	}
	if ae == nil {
		ok, _, e := s.deps.Passwords.Verify(ctx, text(a.PasswordHash), currentPassword)
		if e != nil {
			return e
		}
		if !ok {
			return ErrReauthentication
		}
	} else if currentPassword != "" || reauthID == uuid.Nil {
		return ErrInvalid
	}
	hash, e := s.deps.Passwords.Hash(ctx, newPassword)
	if e != nil {
		return e
	}
	return db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		u, _, e := s.lockSelf(ctx, q, r)
		if e != nil {
			return e
		}
		if u.AuthVersion != before.AuthVersion {
			return ErrReauthentication
		}
		current, ce := q.GetPasswordAccount(ctx, u.ID)
		if ae == nil {
			if ce != nil {
				return proofError(credentialLookupError(ce), ErrReauthentication)
			}
			if current.ID != a.ID || current.Version != a.Version {
				return ErrReauthentication
			}
			if _, e = q.ChangePassword(ctx, sqlc.ChangePasswordParams{ID: a.ID, UserID: u.ID, PasswordHash: &hash}); e != nil {
				return e
			}
		} else {
			if !errors.Is(ce, pgx.ErrNoRows) {
				if ce != nil {
					return ce
				}
				return ErrConflict
			}
			if u.EmailVerifiedAt == nil {
				return ErrInvalid
			}
			proof, e := s.authorization(ctx, q, r, reauthID, OperationSetPassword, CredentialProvider, nil)
			if e != nil {
				return e
			}
			rows, e := q.ListAccounts(ctx, u.ID)
			if e != nil {
				return e
			}
			if len(rows) >= 10 {
				return ErrConflict
			}
			if _, e = q.CreateAccount(ctx, sqlc.CreateAccountParams{UserID: u.ID, ProviderID: CredentialProvider, ProviderAccountID: u.ID.String(), ProviderNamespace: LocalNamespace, PasswordHash: &hash}); e != nil {
				return e
			}
			if e = finishAuthorization(ctx, q, proof); e != nil {
				return e
			}
		}
		if e = s.invalidate(ctx, q, u.ID); e != nil {
			return e
		}
		if err := q.enqueueSecurityNotification(ctx, u, "账户密码已变更，请重新登录。"); err != nil {
			return err
		}
		return audit(ctx, q, r, "account.password_change", AuditSuccess, "user", u.ID.String(), u.ID.String(), "", nil)
	})
}
