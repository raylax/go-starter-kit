package account

import (
	"context"
	"errors"
	"time"

	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/example/go-starter-kit/internal/validation"
	"github.com/jackc/pgx/v5"
)

func (s *Service) validateOperation(operation Operation, target string) error {
	switch operation {
	case OperationLinkAccount:
		if !s.deps.Federation.Enabled(target) {
			return ErrInvalid
		}
	case OperationUnlinkAccount:
		if !validation.Identifier(target) {
			return ErrInvalid
		}
	case OperationSetPassword:
		if target != CredentialProvider {
			return ErrInvalid
		}
	case OperationChangeEmail:
		normalized, err := normalizeEmail(target)
		if err != nil || normalized != target {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// authorization 校验原会话、精确用途、证明账号版本和一次性认领状态。
func (s *Service) authorization(ctx context.Context, q *store, r Request, id string, operation Operation, target string, claim *string) (sqlc.AuthFlow, error) {
	if !validation.Identifier(id) {
		return sqlc.AuthFlow{}, ErrReauthentication
	}
	proof, err := q.queries.GetFlow(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return proof, ErrReauthentication
	}
	if err != nil {
		return proof, err
	}
	if FlowPurpose(proof.Purpose) != FlowReauthenticate || proof.UserID == nil || proof.SessionID == nil || proof.AccountID == nil {
		return proof, ErrReauthentication
	}
	if *proof.UserID != r.UserID || *proof.SessionID != r.SessionID {
		return proof, ErrReauthentication
	}
	if Operation(proof.Operation) != operation || proof.Target != target {
		return proof, ErrReauthentication
	}
	if claim == nil {
		if FlowStatus(proof.Status) != FlowAuthorized {
			return proof, ErrReauthentication
		}
	} else if FlowStatus(proof.Status) != FlowClaimed || proof.ClaimedBy == nil || *proof.ClaimedBy != *claim {
		return proof, ErrReauthentication
	}
	user, err := q.queries.GetUser(ctx, *proof.UserID)
	if err != nil {
		return proof, proofError(credentialLookupError(err), ErrReauthentication)
	}
	if UserStatus(user.Status) != UserActive || user.AuthVersion != proof.AuthVersion {
		return proof, ErrReauthentication
	}
	account, err := q.queries.GetAccount(ctx, sqlc.GetAccountParams{ID: *proof.AccountID, UserID: user.ID})
	if err != nil {
		return proof, proofError(credentialLookupError(err), ErrReauthentication)
	}
	if account.Version != proof.AccountVersion || !s.accountEnabled(account) {
		return proof, ErrReauthentication
	}
	if _, err = q.queries.GetValidSession(ctx, sqlc.GetValidSessionParams{ID: *proof.SessionID, UserID: user.ID}); err != nil {
		return proof, proofError(credentialLookupError(err), ErrReauthentication)
	}
	return proof, nil
}

func finishAuthorization(ctx context.Context, q *store, proof sqlc.AuthFlow) error {
	return proofError(q.consumeFlow(ctx, proof.ID, FlowStatus(proof.Status)), ErrReauthentication)
}

func (s *Service) Reauthenticate(ctx context.Context, r Request, input Reauthentication) (_ ReauthenticationResult, failureErr error) {
	defer s.recordFailureOnReturn(ctx, r, "auth.reauthenticate", &failureErr)
	if err := s.validateOperation(input.Operation, input.Target); err != nil {
		return ReauthenticationResult{}, err
	}
	if err := s.entryLimit(ctx, r, "reauth", r.UserID); err != nil {
		return ReauthenticationResult{}, err
	}
	user, _, err := s.readSelf(ctx, s.store, r)
	if err != nil {
		return ReauthenticationResult{}, db.MapError(err, storageErrors)
	}
	var account sqlc.Account
	if input.Method == MethodPassword {
		account, err = s.store.queries.GetPasswordAccount(ctx, user.ID)
	} else if input.Method == MethodOAuth {
		if !validation.Identifier(input.AccountID) {
			return ReauthenticationResult{}, ErrInvalid
		}
		account, err = s.store.queries.GetAccount(ctx, sqlc.GetAccountParams{ID: input.AccountID, UserID: user.ID})
	} else {
		return ReauthenticationResult{}, ErrInvalid
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ReauthenticationResult{}, ErrReauthentication
	}
	if err != nil {
		return ReauthenticationResult{}, err
	}
	if input.Operation == OperationUnlinkAccount && account.ID == input.Target {
		return ReauthenticationResult{}, ErrLastAccount
	}
	if input.Method == MethodOAuth {
		if account.ProviderID == CredentialProvider || !s.accountEnabled(account) {
			return ReauthenticationResult{}, ErrInvalid
		}
		result, err := s.startReauthenticationFlow(ctx, r, account, input.Operation, input.Target)
		return redirectReauthentication(result), err
	}
	valid, _, err := s.deps.Passwords.Verify(ctx, text(account.PasswordHash), input.Password)
	if err != nil {
		return ReauthenticationResult{}, err
	}
	if !valid {
		return ReauthenticationResult{}, ErrReauthentication
	}
	var result ReauthenticationResult
	err = db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		currentUser, session, err := s.readSelf(ctx, q, r)
		if err != nil {
			return err
		}
		currentAccount, err := q.queries.GetAccount(ctx, sqlc.GetAccountParams{ID: account.ID, UserID: user.ID})
		if err != nil {
			return proofError(credentialLookupError(err), ErrReauthentication)
		}
		if currentAccount.Version != account.Version || currentUser.AuthVersion != user.AuthVersion {
			return ErrReauthentication
		}
		now := time.Now()
		id := identity.NewID()
		_, err = q.CreateFlow(ctx, sqlc.CreateFlowParams{
			ID:              id,
			Purpose:         string(FlowReauthenticate),
			UserID:          &user.ID,
			SessionID:       &session.ID,
			AuthVersion:     user.AuthVersion,
			AccountID:       &account.ID,
			AccountVersion:  account.Version,
			Operation:       string(input.Operation),
			Target:          input.Target,
			Status:          string(FlowAuthorized),
			AuthenticatedAt: &now,
		})
		if err != nil {
			return err
		}
		result = completedReauthentication(id)
		return nil
	})
	return result, err
}
