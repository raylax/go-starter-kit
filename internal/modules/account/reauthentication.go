package account

import (
	"context"
	"errors"
	"time"

	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Service) validateOperation(operation Operation, target string) error {
	switch operation {
	case OperationLinkAccount:
		if !s.deps.Federation.Enabled(target) {
			return ErrInvalid
		}
	case OperationUnlinkAccount:
		if _, e := uuid.Parse(target); e != nil {
			return ErrInvalid
		}
	case OperationSetPassword:
		if target != CredentialProvider {
			return ErrInvalid
		}
	case OperationChangeEmail:
		normalized, e := normalizeEmail(target)
		if e != nil || normalized != target {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// authorization 校验原会话、精确用途、证明账号版本和一次性认领状态。
func (s *Service) authorization(ctx context.Context, q *store, r Request, id uuid.UUID, operation Operation, target string, claim *uuid.UUID) (sqlc.AuthFlow, error) {
	f, e := q.GetFlow(ctx, id)
	if errors.Is(e, pgx.ErrNoRows) {
		return f, ErrReauthentication
	}
	if e != nil {
		return f, e
	}
	if FlowPurpose(f.Purpose) != FlowReauthenticate || f.UserID == nil || f.SessionID == nil || f.UserID.String() != r.UserID || f.SessionID.String() != r.SessionID || Operation(f.Operation) != operation || f.Target != target || f.AccountID == nil {
		return f, ErrReauthentication
	}
	if claim == nil {
		if FlowStatus(f.Status) != FlowAuthorized {
			return f, ErrReauthentication
		}
	} else if FlowStatus(f.Status) != FlowClaimed || f.ClaimedBy == nil || *f.ClaimedBy != *claim {
		return f, ErrReauthentication
	}
	u, e := q.GetUser(ctx, *f.UserID)
	if e != nil {
		return f, proofError(credentialLookupError(e), ErrReauthentication)
	}
	if UserStatus(u.Status) != UserActive || u.AuthVersion != f.AuthVersion {
		return f, ErrReauthentication
	}
	a, e := q.GetAccount(ctx, sqlc.GetAccountParams{ID: *f.AccountID, UserID: u.ID})
	if e != nil {
		return f, proofError(credentialLookupError(e), ErrReauthentication)
	}
	if a.Version != f.AccountVersion || !s.accountEnabled(a) {
		return f, ErrReauthentication
	}
	if _, e = q.GetValidSession(ctx, sqlc.GetValidSessionParams{ID: *f.SessionID, UserID: u.ID}); e != nil {
		return f, proofError(credentialLookupError(e), ErrReauthentication)
	}
	return f, nil
}
func finishAuthorization(ctx context.Context, q *store, f sqlc.AuthFlow) error {
	return proofError(q.consumeFlow(ctx, f.ID, FlowStatus(f.Status)), ErrReauthentication)
}
func (s *Service) Reauthenticate(ctx context.Context, r Request, input Reauthentication) (_ AuthenticationResult, failureErr error) {
	defer s.recordFailureOnReturn(ctx, r, "auth.reauthenticate", &failureErr)
	if e := s.validateOperation(input.Operation, input.Target); e != nil {
		return AuthenticationResult{}, e
	}
	if e := s.entryLimit(ctx, r, "reauth", r.UserID); e != nil {
		return AuthenticationResult{}, e
	}
	u, _, e := s.readSelf(ctx, s.queries, r)
	if e != nil {
		return AuthenticationResult{}, db.MapError(e, storageErrors)
	}
	var a sqlc.Account
	if input.Method == MethodPassword {
		a, e = s.queries.GetPasswordAccount(ctx, u.ID)
	} else if input.Method == MethodOAuth {
		a, e = s.queries.GetAccount(ctx, sqlc.GetAccountParams{ID: input.AccountID, UserID: u.ID})
	} else {
		return AuthenticationResult{}, ErrInvalid
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return AuthenticationResult{}, ErrReauthentication
	}
	if e != nil {
		return AuthenticationResult{}, e
	}
	if input.Operation == OperationUnlinkAccount && a.ID.String() == input.Target {
		return AuthenticationResult{}, ErrLastAccount
	}
	if input.Method == MethodOAuth {
		if a.ProviderID == CredentialProvider || !s.accountEnabled(a) {
			return AuthenticationResult{}, ErrInvalid
		}
		result, e := s.startFlow(ctx, r, FlowReauthenticate, a.ProviderID, input.Operation, input.Target, &a, nil)
		return AuthenticationResult{Result: ResultRedirect, Flow: result}, e
	}
	valid, _, e := s.deps.Passwords.Verify(ctx, text(a.PasswordHash), input.Password)
	if e != nil {
		return AuthenticationResult{}, e
	}
	if !valid {
		return AuthenticationResult{}, ErrReauthentication
	}
	var result AuthenticationResult
	e = db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		fresh, session, e := s.readSelf(ctx, q, r)
		if e != nil {
			return e
		}
		current, e := q.GetAccount(ctx, sqlc.GetAccountParams{ID: a.ID, UserID: u.ID})
		if e != nil {
			return proofError(credentialLookupError(e), ErrReauthentication)
		}
		if current.Version != a.Version || fresh.AuthVersion != u.AuthVersion {
			return ErrReauthentication
		}
		now := time.Now()
		id := uuid.New()
		_, e = q.CreateFlow(ctx, sqlc.CreateFlowParams{ID: id, Purpose: string(FlowReauthenticate), UserID: &u.ID, SessionID: &session.ID, AuthVersion: u.AuthVersion, AccountID: &a.ID, AccountVersion: a.Version, Operation: string(input.Operation), Target: input.Target, Status: string(FlowAuthorized), AuthenticatedAt: &now})
		if e != nil {
			return e
		}
		result = AuthenticationResult{Result: ResultReauthenticated, ReauthenticationID: id}
		return nil
	})
	return result, e
}
