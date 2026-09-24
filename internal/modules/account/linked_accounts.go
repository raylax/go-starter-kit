package account

import (
	"context"
	"errors"

	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	maxLoginAccounts = 10

	accountLinkedNotification   = "新的第三方登录账号已绑定。"
	accountUnlinkedNotification = "一个登录账号已解除绑定，请使用保留的方式重新登录。"
)

func (s *Service) ConfirmLink(ctx context.Context, r Request, flowID uuid.UUID) (failureErr error) {
	defer func() {
		failureErr = proofError(failureErr, ErrFlow)
		s.recordFailure(ctx, r, "account.link", failureErr)
	}()
	return db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		u, session, e := s.lockSelf(ctx, q, r)
		if e != nil {
			return e
		}
		f, e := q.GetFlow(ctx, flowID)
		if e != nil {
			return credentialLookupError(e)
		}
		if FlowPurpose(f.Purpose) != FlowLinkIdentity || f.UserID == nil || *f.UserID != u.ID || f.SessionID == nil || *f.SessionID != session.ID || f.AuthVersion != u.AuthVersion || f.ReauthenticationID == nil {
			return ErrCredentials
		}
		if FlowStatus(f.Status) == FlowConsumed {
			a, e := q.FindProviderAccount(ctx, sqlc.FindProviderAccountParams{ProviderNamespace: f.VerifiedNamespace, ProviderAccountID: f.VerifiedSubject})
			if e != nil {
				return credentialLookupError(e)
			}
			if a.UserID == u.ID {
				return nil
			}
			return ErrCredentials
		}
		if FlowStatus(f.Status) != FlowVerified || s.deps.Federation.Version(f.ProviderID) != f.ConfigVersion {
			return ErrCredentials
		}
		proof, e := s.authorization(ctx, q, r, *f.ReauthenticationID, OperationLinkAccount, f.ProviderID, &f.ID)
		if e != nil {
			return e
		}
		a, e := q.FindProviderAccount(ctx, sqlc.FindProviderAccountParams{ProviderNamespace: f.VerifiedNamespace, ProviderAccountID: f.VerifiedSubject})
		if e == nil {
			if a.UserID != u.ID {
				return ErrConflict
			}
		} else if errors.Is(e, pgx.ErrNoRows) {
			rows, e := q.ListAccounts(ctx, u.ID)
			if e != nil {
				return e
			}
			if len(rows) >= maxLoginAccounts {
				return ErrConflict
			}
			a, e = q.CreateAccount(ctx, sqlc.CreateAccountParams{UserID: u.ID, ProviderID: f.ProviderID, ProviderNamespace: f.VerifiedNamespace, ProviderAccountID: f.VerifiedSubject})
			if e != nil {
				return e
			}
		} else {
			return e
		}
		if e = finishAuthorization(ctx, q, proof); e != nil {
			return e
		}
		if e = finishAuthorization(ctx, q, f); e != nil {
			return e
		}
		if err := q.enqueueSecurityNotification(ctx, u, accountLinkedNotification); err != nil {
			return err
		}
		metadata := accountProviderAuditMetadata{Provider: f.ProviderID}
		return audit(ctx, q, r, "account.link", AuditSuccess, "account", a.ID.String(), u.ID.String(), "", metadata)
	})
}
func (s *Service) Unlink(ctx context.Context, r Request, id, reauthID uuid.UUID) (failureErr error) {
	defer s.recordFailureOnReturn(ctx, r, "account.unlink", &failureErr)
	return db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		u, _, e := s.lockSelf(ctx, q, r)
		if e != nil {
			return e
		}
		a, e := q.GetAccount(ctx, sqlc.GetAccountParams{ID: id, UserID: u.ID})
		if e != nil {
			return e
		}
		proof, e := s.authorization(ctx, q, r, reauthID, OperationUnlinkAccount, id.String(), nil)
		if e != nil {
			return e
		}
		if proof.AccountID == nil || *proof.AccountID == id {
			return ErrLastAccount
		}
		rows, e := q.ListAccounts(ctx, u.ID)
		if e != nil {
			return e
		}
		usable := 0
		for _, row := range rows {
			if row.ID != id && s.accountEnabled(row) {
				usable++
			}
		}
		if usable == 0 {
			return ErrLastAccount
		}
		if _, e = q.RevokeAccount(ctx, sqlc.RevokeAccountParams{ID: id, UserID: u.ID}); e != nil {
			return e
		}
		if e = finishAuthorization(ctx, q, proof); e != nil {
			return e
		}
		if e = s.invalidate(ctx, q, u.ID); e != nil {
			return e
		}
		if err := q.enqueueSecurityNotification(ctx, u, accountUnlinkedNotification); err != nil {
			return err
		}
		metadata := accountProviderAuditMetadata{Provider: a.ProviderID}
		return audit(ctx, q, r, "account.unlink", AuditSuccess, "account", id.String(), u.ID.String(), "", metadata)
	})
}

// accountEnabled 判断未撤销账号的登录能力是否仍启用。
func (s *Service) accountEnabled(a sqlc.Account) bool {
	return a.ProviderID == CredentialProvider || (s.deps.Federation.Enabled(a.ProviderID))
}
