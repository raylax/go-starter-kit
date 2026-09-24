package account

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/go-starter-kit/internal/apperror"
	"github.com/example/go-starter-kit/internal/authorization"
	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	maxOAuthCodeBytes  = 8 << 10 // 8 KiB
	maxOAuthStateBytes = 128
)

type protocolState struct{ Verifier string }

func (s *Service) StartLogin(ctx context.Context, r Request, provider string, purpose FlowPurpose) (FlowResult, error) {
	if !purpose.PublicStart() {
		return FlowResult{}, ErrInvalid
	}
	if e := s.limit(ctx, "oauth.ip", r.ClientIP, ipRequestLimit, ipRateWindow); e != nil {
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
	if !s.deps.Federation.Enabled(provider) {
		return FlowResult{}, ErrInvalid
	}
	id := uuid.New()
	token, hash := identity.NewToken(FlowPrefix)
	state, stateHash := identity.NewToken("")
	verifier, _ := identity.NewToken("")
	authorizationURL, version, e := s.deps.Federation.Start(ctx, provider, state, verifier)
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
			u, session, e := s.readSelf(ctx, q, r)
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
		if purpose == FlowLinkIdentity {
			// 先插入流程完成外键检查，再以条件更新认领；竞争失败时整体回滚。
			if e := q.claimAuthorization(ctx, *reauthID, id); e != nil {
				return proofError(e, ErrReauthentication)
			}
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
	if e := s.limit(ctx, "callback.ip", r.ClientIP, ipRequestLimit, ipRateWindow); e != nil {
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
	if FlowStatus(f.Status) != FlowPending || len(code) == 0 || len(code) > maxOAuthCodeBytes || len(state) > maxOAuthStateBytes || subtle.ConstantTimeCompare(identity.Digest(state), f.StateHash) != 1 {
		return AuthenticationResult{}, ErrCredentials
	}
	if s.deps.Federation.Version(f.ProviderID) != f.ConfigVersion {
		return AuthenticationResult{}, ErrCredentials
	}
	if e := s.queries.claimPendingFlow(ctx, f.ID); e != nil {
		return AuthenticationResult{}, e
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
	verified, e := s.deps.Federation.Verify(ctx, f.ProviderID, f.ConfigVersion, code, protocol.Verifier)
	if e != nil {
		return AuthenticationResult{}, e
	}
	if verified.Subject == "" || len(verified.Subject) > maxProviderSubjectBytes || verified.Namespace == "" || len(verified.Namespace) > maxProviderNamespaceBytes {
		return AuthenticationResult{}, ErrCredentials
	}
	if !utf8.ValidString(verified.Name) {
		verified.Name = ""
	}
	name := []rune(strings.TrimSpace(verified.Name))
	if len(name) > maxDisplayNameRunes {
		name = name[:maxDisplayNameRunes]
	}
	verified.Name = string(name)
	var result AuthenticationResult
	switch FlowPurpose(f.Purpose) {
	case FlowLogin, FlowRegister:
		result, e = s.completeLogin(ctx, r, f, verified)
	case FlowReauthenticate, FlowLinkIdentity:
		result, e = s.completeBoundFlow(ctx, r, f, verified)
	default:
		e = ErrCredentials
	}
	completed = e == nil
	return result, e
}

// completeBoundFlow 在同一事务内复核原会话，并完成已绑定用户的证明。
func (s *Service) completeBoundFlow(ctx context.Context, r Request, f sqlc.AuthFlow, verified VerifiedIdentity) (AuthenticationResult, error) {
	var result AuthenticationResult
	err := db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		if f.UserID == nil || f.SessionID == nil {
			return ErrCredentials
		}
		bound := Request{Subject: authorization.Subject{UserID: f.UserID.String(), SessionID: f.SessionID.String()}, RequestID: r.RequestID}
		u, _, err := s.readSelf(ctx, q, bound)
		if errors.Is(err, apperror.ErrUnauthenticated) {
			return ErrFlow
		}
		if err != nil {
			return err
		}
		if u.AuthVersion != f.AuthVersion {
			return ErrCredentials
		}
		switch FlowPurpose(f.Purpose) {
		case FlowReauthenticate:
			result, err = s.completeReauthentication(ctx, q, u, f, verified)
		case FlowLinkIdentity:
			result, err = s.completeLinkProof(ctx, q, bound, f, verified)
		default:
			err = ErrCredentials
		}
		return err
	})
	return result, err
}

func (s *Service) completeReauthentication(ctx context.Context, q *store, u sqlc.User, f sqlc.AuthFlow, v VerifiedIdentity) (AuthenticationResult, error) {
	if f.AccountID == nil {
		return AuthenticationResult{}, ErrCredentials
	}
	a, err := q.GetAccount(ctx, sqlc.GetAccountParams{ID: *f.AccountID, UserID: u.ID})
	if err != nil {
		return AuthenticationResult{}, credentialLookupError(err)
	}
	if a.Version != f.AccountVersion || a.ProviderNamespace != v.Namespace || a.ProviderAccountID != v.Subject || !s.accountEnabled(a) {
		return AuthenticationResult{}, ErrCredentials
	}
	if err := q.verifyFlow(ctx, f.ID, FlowAuthorized, v); err != nil {
		return AuthenticationResult{}, err
	}
	return AuthenticationResult{Result: ResultReauthenticated, ReauthenticationID: f.ID}, nil
}

func (s *Service) completeLinkProof(ctx context.Context, q *store, bound Request, f sqlc.AuthFlow, v VerifiedIdentity) (AuthenticationResult, error) {
	if f.ReauthenticationID == nil {
		return AuthenticationResult{}, ErrCredentials
	}
	if _, err := s.authorization(ctx, q, bound, *f.ReauthenticationID, OperationLinkAccount, f.ProviderID, &f.ID); err != nil {
		return AuthenticationResult{}, err
	}
	if err := q.verifyFlow(ctx, f.ID, FlowVerified, v); err != nil {
		return AuthenticationResult{}, err
	}
	return AuthenticationResult{Result: ResultLinkPending, Flow: FlowResult{ID: f.ID}, Provider: f.ProviderID, Name: v.Name}, nil
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
		if e := q.consumeFlow(ctx, f.ID, FlowProcessing); e != nil {
			return e
		}
		session, e := s.newSession(ctx, q, r, u, a, MethodOAuth)
		result = AuthenticationResult{Result: ResultSession, Session: session}
		return e
	})
	return result, e
}
