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
	if err := s.entryLimit(ctx, r, "password", r.UserID); err != nil {
		return err
	}
	userBefore, _, err := s.readSelf(ctx, s.queries, r)
	if err != nil {
		return db.MapError(err, storageErrors)
	}
	passwordAccount, accountErr := s.queries.GetPasswordAccount(ctx, userBefore.ID)
	if accountErr != nil && !errors.Is(accountErr, pgx.ErrNoRows) {
		return accountErr
	}
	hasPassword := accountErr == nil
	if hasPassword {
		matches, _, err := s.deps.Passwords.Verify(ctx, text(passwordAccount.PasswordHash), currentPassword)
		if err != nil {
			return err
		}
		if !matches {
			return ErrReauthentication
		}
	} else if currentPassword != "" || reauthID == uuid.Nil {
		return ErrInvalid
	}
	passwordHash, err := s.deps.Passwords.Hash(ctx, newPassword)
	if err != nil {
		return err
	}
	return db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		user, _, err := s.lockSelf(ctx, q, r)
		if err != nil {
			return err
		}
		if user.AuthVersion != userBefore.AuthVersion {
			return ErrReauthentication
		}
		currentAccount, lookupErr := q.GetPasswordAccount(ctx, user.ID)
		if hasPassword {
			if lookupErr != nil {
				return proofError(credentialLookupError(lookupErr), ErrReauthentication)
			}
			if currentAccount.ID != passwordAccount.ID || currentAccount.Version != passwordAccount.Version {
				return ErrReauthentication
			}
			if _, err := q.ChangePassword(ctx, sqlc.ChangePasswordParams{ID: passwordAccount.ID, UserID: user.ID, PasswordHash: &passwordHash}); err != nil {
				return err
			}
		} else {
			if lookupErr == nil {
				return ErrConflict
			}
			if !errors.Is(lookupErr, pgx.ErrNoRows) {
				return lookupErr
			}
			if user.EmailVerifiedAt == nil {
				return ErrInvalid
			}
			proof, err := s.authorization(ctx, q, r, reauthID, OperationSetPassword, CredentialProvider, nil)
			if err != nil {
				return err
			}
			accounts, err := q.ListAccounts(ctx, user.ID)
			if err != nil {
				return err
			}
			if len(accounts) >= maxLoginAccounts {
				return ErrConflict
			}
			if _, err := q.CreateAccount(ctx, sqlc.CreateAccountParams{UserID: user.ID, ProviderID: CredentialProvider, ProviderAccountID: user.ID.String(), ProviderNamespace: LocalNamespace, PasswordHash: &passwordHash}); err != nil {
				return err
			}
			if err := finishAuthorization(ctx, q, proof); err != nil {
				return err
			}
		}
		if err := s.invalidate(ctx, q, user.ID); err != nil {
			return err
		}
		if err := enqueueSecurityNotification(ctx, q, user, passwordChangedNotification); err != nil {
			return err
		}
		return audit(ctx, q, r, auditEvent{
			Action:       "account.password_change",
			Outcome:      AuditSuccess,
			ResourceType: "user",
			ResourceID:   user.ID.String(),
			ScopeSubject: user.ID.String(),
		})
	})
}
