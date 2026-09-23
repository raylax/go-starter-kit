package account

import (
	"context"
	"errors"
	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

func (s *Service) validateOperation(operation Operation, target string) error {
	switch operation {
	case OperationLinkAccount:
		if s.deps.ProviderEnabled == nil || !s.deps.ProviderEnabled(target) {
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
	f, e := q.LockFlow(ctx, id)
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
	count, e := q.FinishFlow(ctx, sqlc.FinishFlowParams{ID: f.ID, Status: f.Status})
	if e != nil {
		return e
	}
	if count != 1 {
		return ErrReauthentication
	}
	return nil
}

func (s *Service) Reauthenticate(ctx context.Context, r Request, input Reauthentication) (_ AuthenticationResult, failureErr error) {
	defer s.recordFailureOnReturn(ctx, r, "auth.reauthenticate", &failureErr)
	if e := s.validateOperation(input.Operation, input.Target); e != nil {
		return AuthenticationResult{}, e
	}
	if e := s.entryLimit(ctx, r, "reauth", r.UserID); e != nil {
		return AuthenticationResult{}, e
	}
	profile, e := s.Me(ctx, r)
	if e != nil {
		return AuthenticationResult{}, e
	}
	u, e := s.queries.GetUser(ctx, profile.User.ID)
	if e != nil {
		return AuthenticationResult{}, e
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
	valid, _, e := s.deps.Verify(ctx, text(a.PasswordHash), input.Password)
	if e != nil {
		return AuthenticationResult{}, e
	}
	if !valid {
		return AuthenticationResult{}, ErrReauthentication
	}
	var result AuthenticationResult
	e = db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		fresh, session, e := s.lockSelf(ctx, q, r)
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
		id := uuid.Must(uuid.NewV7())
		_, e = q.CreateFlow(ctx, sqlc.CreateFlowParams{ID: id, Purpose: string(FlowReauthenticate), UserID: &u.ID, SessionID: &session.ID, AuthVersion: u.AuthVersion, AccountID: &a.ID, AccountVersion: a.Version, Operation: string(input.Operation), Target: input.Target, Status: string(FlowAuthorized), AuthenticatedAt: &now})
		if e != nil {
			return e
		}
		result = AuthenticationResult{Result: ResultReauthenticated, ReauthenticationID: id}
		return nil
	})
	return result, e
}

func (s *Service) SetPassword(ctx context.Context, r Request, currentPassword, newPassword string, reauthID uuid.UUID) (failureErr error) {
	defer s.recordFailureOnReturn(ctx, r, "account.password_change", &failureErr)
	if !s.deps.ValidPassword(newPassword) {
		return ErrInvalid
	}
	if e := s.entryLimit(ctx, r, "password", r.UserID); e != nil {
		return e
	}
	profile, e := s.Me(ctx, r)
	if e != nil {
		return e
	}
	before, e := s.queries.GetUser(ctx, profile.User.ID)
	if e != nil {
		return e
	}
	a, ae := s.queries.GetPasswordAccount(ctx, before.ID)
	if ae != nil && !errors.Is(ae, pgx.ErrNoRows) {
		return ae
	}
	if ae == nil {
		ok, _, e := s.deps.Verify(ctx, text(a.PasswordHash), currentPassword)
		if e != nil {
			return e
		}
		if !ok {
			return ErrReauthentication
		}
	} else if currentPassword != "" || reauthID == uuid.Nil {
		return ErrInvalid
	}
	hash, e := s.deps.Hash(ctx, newPassword)
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
		if err := q.enqueueSecurityNotification(ctx, u, "一个登录账号已解除绑定，请使用保留的方式重新登录。"); err != nil {
			return err
		}
		return audit(ctx, q, r, "account.unlink", AuditSuccess, "account", id.String(), u.ID.String(), "", struct {
			Provider string `json:"provider"`
		}{a.ProviderID})
	})
}
