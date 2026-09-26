package account

import (
	"context"
	"errors"

	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxLoginAccounts = 10

func (s *Service) ConfirmLink(ctx context.Context, r Request, flowID uuid.UUID) (failureErr error) {
	defer func() {
		failureErr = proofError(failureErr, ErrFlow)
		s.recordFailure(ctx, r, "account.link", failureErr)
	}()
	return db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		user, session, err := s.lockSelf(ctx, q, r)
		if err != nil {
			return err
		}
		flow, err := q.GetFlow(ctx, flowID)
		if err != nil {
			return credentialLookupError(err)
		}
		if FlowPurpose(flow.Purpose) != FlowLinkIdentity || flow.UserID == nil || flow.SessionID == nil || flow.ReauthenticationID == nil {
			return ErrCredentials
		}
		if *flow.UserID != user.ID || *flow.SessionID != session.ID || flow.AuthVersion != user.AuthVersion {
			return ErrCredentials
		}
		if FlowStatus(flow.Status) == FlowConsumed {
			account, err := q.FindProviderAccount(ctx, sqlc.FindProviderAccountParams{ProviderNamespace: flow.VerifiedNamespace, ProviderAccountID: flow.VerifiedSubject})
			if err != nil {
				return credentialLookupError(err)
			}
			if account.UserID == user.ID {
				return nil
			}
			return ErrCredentials
		}
		if FlowStatus(flow.Status) != FlowVerified || s.deps.Federation.Version(flow.ProviderID) != flow.ConfigVersion {
			return ErrCredentials
		}
		proof, err := s.authorization(ctx, q, r, *flow.ReauthenticationID, OperationLinkAccount, flow.ProviderID, &flow.ID)
		if err != nil {
			return err
		}
		account, err := q.FindProviderAccount(ctx, sqlc.FindProviderAccountParams{ProviderNamespace: flow.VerifiedNamespace, ProviderAccountID: flow.VerifiedSubject})
		if err == nil {
			if account.UserID != user.ID {
				return ErrConflict
			}
		} else if errors.Is(err, pgx.ErrNoRows) {
			rows, err := q.ListAccounts(ctx, user.ID)
			if err != nil {
				return err
			}
			if len(rows) >= maxLoginAccounts {
				return ErrConflict
			}
			account, err = q.CreateAccount(ctx, sqlc.CreateAccountParams{
				UserID:            user.ID,
				ProviderID:        flow.ProviderID,
				ProviderNamespace: flow.VerifiedNamespace,
				ProviderAccountID: flow.VerifiedSubject,
			})
			if err != nil {
				return err
			}
		} else {
			return err
		}
		if err = finishAuthorization(ctx, q, proof); err != nil {
			return err
		}
		if err = finishAuthorization(ctx, q, flow); err != nil {
			return err
		}
		if err := enqueueSecurityNotification(ctx, q, user, accountLinkedNotification); err != nil {
			return err
		}
		metadata := accountProviderAuditMetadata{Provider: flow.ProviderID}
		return audit(ctx, q, r, auditEvent{
			Action:       "account.link",
			Outcome:      AuditSuccess,
			ResourceType: "account",
			ResourceID:   account.ID.String(),
			ScopeSubject: user.ID.String(),
			Metadata:     metadata,
		})
	})
}

func (s *Service) Unlink(ctx context.Context, r Request, id, reauthID uuid.UUID) (failureErr error) {
	defer s.recordFailureOnReturn(ctx, r, "account.unlink", &failureErr)
	return db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		user, _, err := s.lockSelf(ctx, q, r)
		if err != nil {
			return err
		}
		account, err := q.GetAccount(ctx, sqlc.GetAccountParams{ID: id, UserID: user.ID})
		if err != nil {
			return err
		}
		proof, err := s.authorization(ctx, q, r, reauthID, OperationUnlinkAccount, id.String(), nil)
		if err != nil {
			return err
		}
		if proof.AccountID == nil || *proof.AccountID == id {
			return ErrLastAccount
		}
		rows, err := q.ListAccounts(ctx, user.ID)
		if err != nil {
			return err
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
		if _, err = q.RevokeAccount(ctx, sqlc.RevokeAccountParams{ID: id, UserID: user.ID}); err != nil {
			return err
		}
		if err = finishAuthorization(ctx, q, proof); err != nil {
			return err
		}
		if err = s.invalidate(ctx, q, user.ID); err != nil {
			return err
		}
		if err := enqueueSecurityNotification(ctx, q, user, accountUnlinkedNotification); err != nil {
			return err
		}
		metadata := accountProviderAuditMetadata{Provider: account.ProviderID}
		return audit(ctx, q, r, auditEvent{
			Action:       "account.unlink",
			Outcome:      AuditSuccess,
			ResourceType: "account",
			ResourceID:   id.String(),
			ScopeSubject: user.ID.String(),
			Metadata:     metadata,
		})
	})
}

// accountEnabled 判断未撤销账号的登录能力是否仍启用。
func (s *Service) accountEnabled(account sqlc.Account) bool {
	return account.ProviderID == CredentialProvider || (s.deps.Federation.Enabled(account.ProviderID))
}
