package account

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/example/go-starter-kit/internal/authorization"
	"github.com/example/go-starter-kit/internal/db"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/go-starter-kit/internal/apperror"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type protocolState struct{ Verifier string }

func (s *Service) StartLogin(ctx context.Context, r Request, provider string, purpose FlowPurpose) (FlowResult, error) {
	if !purpose.PublicStart() {
		return FlowResult{}, ErrInvalid
	}
	if e := s.entryLimit(ctx, r, "oauth", r.ClientIP); e != nil {
		return FlowResult{}, e
	}
	return s.startFlow(ctx, r, purpose, provider, "", "", nil, nil)
}
func (s *Service) StartLink(ctx context.Context, r Request, provider string, reauthID uuid.UUID) (FlowResult, error) {
	if e := s.entryLimit(ctx, r, "link", r.UserID); e != nil {
		return FlowResult{}, e
	}
	return s.startFlow(ctx, r, FlowLinkIdentity, provider, OperationLinkAccount, provider, nil, &reauthID)
}
func (s *Service) startFlow(ctx context.Context, r Request, purpose FlowPurpose, provider string, operation Operation, target string, a *sqlc.Account, reauthID *uuid.UUID) (FlowResult, error) {
	if s.deps.ProviderEnabled == nil || !s.deps.ProviderEnabled(provider) {
		return FlowResult{}, ErrInvalid
	}
	if s.deps.StartProvider == nil {
		return FlowResult{}, ErrUnavailable
	}
	if purpose == FlowLinkIdentity || purpose == FlowReauthenticate {
		if _, e := s.Me(ctx, r); e != nil {
			return FlowResult{}, e
		}
	}
	id := uuid.Must(uuid.NewV7())
	token, hash := identity.NewToken(FlowPrefix)
	state, stateHash := identity.NewToken("")
	verifier, _ := identity.NewToken("")
	authorizationURL, version, e := s.deps.StartProvider(ctx, provider, state, verifier)
	if e != nil {
		return FlowResult{}, e
	}
	// 协议状态以 JSON 原文短期保存，流程完成或失败时清除。
	protocol, _ := json.Marshal(protocolState{Verifier: verifier})
	var result FlowResult
	e = db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		params := sqlc.CreateFlowParams{ID: id, Purpose: string(purpose), TokenHash: hash, ProviderID: provider, ConfigVersion: version, StateHash: stateHash, ProtocolState: protocol, Status: string(FlowPending), Operation: string(operation), Target: target, ReauthenticationID: reauthID}
		if purpose == FlowLinkIdentity || purpose == FlowReauthenticate {
			u, session, e := s.lockSelf(ctx, q, r)
			if e != nil {
				return e
			}
			params.UserID = &u.ID
			params.SessionID = &session.ID
			params.AuthVersion = u.AuthVersion
			if purpose == FlowLinkIdentity {
				if reauthID == nil {
					return ErrCredentials
				}
				if _, e = s.authorization(ctx, q, r, *reauthID, OperationLinkAccount, provider, nil); e != nil {
					return e
				}
				count, e := q.ClaimAuthorization(ctx, sqlc.ClaimAuthorizationParams{ID: *reauthID, ClaimedBy: &id})
				if e != nil {
					return e
				}
				if count != 1 {
					return ErrCredentials
				}
			} else {
				if a == nil {
					return ErrCredentials
				}
				current, e := q.GetAccount(ctx, sqlc.GetAccountParams{ID: a.ID, UserID: u.ID})
				if e != nil {
					return credentialLookupError(e)
				}
				if current.Version != a.Version || !s.accountEnabled(current) {
					return ErrCredentials
				}
				params.AccountID = &a.ID
				params.AccountVersion = a.Version
			}
		}
		row, e := q.CreateFlow(ctx, params)
		if e != nil {
			return e
		}
		result = FlowResult{ID: id, Token: token, AuthorizationURL: authorizationURL, ExpiresAt: row.ExpiresAt}
		return nil
	})
	return result, e
}

func (s *Service) Callback(ctx context.Context, r Request, token, code, state string) (_ AuthenticationResult, failureErr error) {
	defer func() {
		failureErr = proofError(failureErr, ErrFlow)
		s.recordFailure(ctx, r, "auth.oauth_callback", failureErr)
	}()
	if e := s.limit(ctx, "callback.ip", r.ClientIP, 60, time.Minute); e != nil {
		return AuthenticationResult{}, e
	}
	hash, e := identity.TokenDigest(token, FlowPrefix)
	if e != nil {
		return AuthenticationResult{}, ErrCredentials
	}
	f, e := s.queries.FindFlow(ctx, hash)
	if errors.Is(e, pgx.ErrNoRows) {
		return AuthenticationResult{}, ErrCredentials
	}
	if e != nil {
		return AuthenticationResult{}, e
	}
	if FlowStatus(f.Status) != FlowPending || len(code) == 0 || len(code) > 8192 || len(state) > 128 || subtle.ConstantTimeCompare(identity.Digest(state), f.StateHash) != 1 {
		return AuthenticationResult{}, ErrCredentials
	}
	if s.deps.VerifyProvider == nil || s.deps.ProviderVersion == nil || s.deps.ProviderVersion(f.ProviderID) != f.ConfigVersion {
		return AuthenticationResult{}, ErrCredentials
	}
	count, e := s.queries.ClaimFlow(ctx, f.ID)
	if e != nil {
		return AuthenticationResult{}, e
	}
	if count != 1 {
		return AuthenticationResult{}, ErrCredentials
	}
	completed := false
	defer func() {
		if !completed {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancel()
			_ = s.queries.FailFlow(cleanup, f.ID)
		}
	}()
	var protocol protocolState
	if json.Unmarshal(f.ProtocolState, &protocol) != nil {
		return AuthenticationResult{}, ErrCredentials
	}
	verified, e := s.deps.VerifyProvider(ctx, f.ProviderID, f.ConfigVersion, code, protocol.Verifier)
	if e != nil {
		return AuthenticationResult{}, e
	}
	if verified.Subject == "" || len(verified.Subject) > 1024 || verified.Namespace == "" || len(verified.Namespace) > 2048 {
		return AuthenticationResult{}, ErrCredentials
	}
	if !utf8.ValidString(verified.Name) {
		verified.Name = ""
	}
	name := []rune(strings.TrimSpace(verified.Name))
	if len(name) > 100 {
		name = name[:100]
	}
	verified.Name = string(name)
	var result AuthenticationResult
	if FlowPurpose(f.Purpose) == FlowLogin || FlowPurpose(f.Purpose) == FlowRegister {
		result, e = s.completeLogin(ctx, r, f, verified)
	} else {
		e = db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
			q := newStore(tx)
			if f.UserID == nil || f.SessionID == nil {
				return ErrCredentials
			}
			bound := Request{Subject: authorization.Subject{UserID: f.UserID.String(), SessionID: f.SessionID.String()}, RequestID: r.RequestID}
			u, _, e := s.lockSelf(ctx, q, bound)
			if errors.Is(e, apperror.ErrUnauthenticated) {
				return ErrFlow
			}
			if e != nil {
				return e
			}
			if u.AuthVersion != f.AuthVersion {
				return ErrCredentials
			}
			status := FlowVerified
			if FlowPurpose(f.Purpose) == FlowReauthenticate {
				if f.AccountID == nil {
					return ErrCredentials
				}
				a, e := q.GetAccount(ctx, sqlc.GetAccountParams{ID: *f.AccountID, UserID: u.ID})
				if e != nil {
					return credentialLookupError(e)
				}
				if a.Version != f.AccountVersion || a.ProviderNamespace != verified.Namespace || a.ProviderAccountID != verified.Subject || !s.accountEnabled(a) {
					return ErrCredentials
				}
				status = FlowAuthorized
				result = AuthenticationResult{Result: ResultReauthenticated, ReauthenticationID: f.ID}
			} else if FlowPurpose(f.Purpose) == FlowLinkIdentity {
				if f.ReauthenticationID == nil {
					return ErrCredentials
				}
				if _, e = s.authorization(ctx, q, bound, *f.ReauthenticationID, OperationLinkAccount, f.ProviderID, &f.ID); e != nil {
					return e
				}
				result = AuthenticationResult{Result: ResultLinkPending, Flow: FlowResult{ID: f.ID}, Provider: f.ProviderID, Name: verified.Name}
			} else {
				return ErrCredentials
			}
			count, e := q.VerifyFlow(ctx, sqlc.VerifyFlowParams{ID: f.ID, Status: string(status), VerifiedNamespace: verified.Namespace, VerifiedSubject: verified.Subject, VerifiedName: verified.Name, AuthenticatedAt: ptr(verified.AuthenticatedAt)})
			if e != nil {
				return e
			}
			if count != 1 {
				return ErrCredentials
			}
			return nil
		})
	}
	completed = e == nil
	return result, e
}
func (s *Service) completeLogin(ctx context.Context, r Request, f sqlc.AuthFlow, v VerifiedIdentity) (AuthenticationResult, error) {
	a, findErr := s.queries.FindProviderAccount(ctx, sqlc.FindProviderAccountParams{ProviderNamespace: v.Namespace, ProviderAccountID: v.Subject})
	if findErr != nil && !errors.Is(findErr, pgx.ErrNoRows) {
		return AuthenticationResult{}, findErr
	}
	if errors.Is(findErr, pgx.ErrNoRows) && FlowPurpose(f.Purpose) != FlowRegister {
		return AuthenticationResult{}, ErrRegistrationRequired
	}
	var result AuthenticationResult
	e := db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		var u sqlc.User
		var e error
		if findErr == nil {
			u, e = q.LockUser(ctx, a.UserID)
			if e != nil {
				return e
			}
			current, e := q.GetAccount(ctx, sqlc.GetAccountParams{ID: a.ID, UserID: a.UserID})
			if e != nil {
				return credentialLookupError(e)
			}
			if current.Version != a.Version || UserStatus(u.Status) != UserActive || !s.accountEnabled(current) {
				return ErrCredentials
			}
			a = current
		} else {
			u, e = q.CreateFederatedUser(ctx, v.Name)
			if e != nil {
				return e
			}
			a, e = q.CreateAccount(ctx, sqlc.CreateAccountParams{UserID: u.ID, ProviderID: f.ProviderID, ProviderNamespace: v.Namespace, ProviderAccountID: v.Subject})
			if e != nil {
				return e
			}
			if e = audit(ctx, q, Request{Subject: authorization.Subject{UserID: u.ID.String()}, RequestID: r.RequestID}, "auth.register", AuditSuccess, "user", u.ID.String(), u.ID.String(), "", nil); e != nil {
				return e
			}
		}
		count, e := q.FinishFlow(ctx, sqlc.FinishFlowParams{ID: f.ID, Status: string(FlowProcessing)})
		if e != nil {
			return e
		}
		if count != 1 {
			return ErrCredentials
		}
		session, e := s.newSession(ctx, q, r, u, a, MethodOAuth)
		result = AuthenticationResult{Result: ResultSession, Session: session}
		return e
	})
	return result, e
}
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
		f, e := q.LockFlow(ctx, flowID)
		if e != nil {
			return credentialLookupError(e)
		}
		if FlowPurpose(f.Purpose) != FlowLinkIdentity || f.UserID == nil || *f.UserID != u.ID || f.SessionID == nil || *f.SessionID != session.ID || f.AuthVersion != u.AuthVersion || f.ReauthenticationID == nil {
			return ErrCredentials
		}
		if FlowStatus(f.Status) == FlowConsumed {
			a, e := q.FindProviderAccount(ctx, sqlc.FindProviderAccountParams{ProviderNamespace: f.VerifiedNamespace, ProviderAccountID: f.VerifiedSubject})
			if e == nil && a.UserID == u.ID {
				return nil
			}
			return ErrCredentials
		}
		if FlowStatus(f.Status) != FlowVerified || s.deps.ProviderVersion == nil || s.deps.ProviderVersion(f.ProviderID) != f.ConfigVersion {
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
			if len(rows) >= 10 {
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
		if err := q.enqueueSecurityNotification(ctx, u, "新的第三方登录账号已绑定。"); err != nil {
			return err
		}
		return audit(ctx, q, r, "account.link", AuditSuccess, "account", a.ID.String(), u.ID.String(), "", struct {
			Provider string `json:"provider"`
		}{f.ProviderID})
	})
}
