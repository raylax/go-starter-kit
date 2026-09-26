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
	"github.com/jackc/pgx/v5"
)

const (
	maxOAuthCodeBytes  = 8 << 10 // 8 KiB
	maxOAuthStateBytes = 128
)

type protocolState struct{ Verifier string }

func (s *Service) Callback(ctx context.Context, r Request, token, code, state string) (_ CallbackResult, failureErr error) {
	defer func() {
		failureErr = proofError(failureErr, ErrFlow)
		s.recordFailure(ctx, r, "auth.oauth_callback", failureErr)
	}()
	if err := s.limit(ctx, "callback.ip", r.ClientIP, ipRequestLimit, ipRateWindow); err != nil {
		return CallbackResult{}, err
	}
	hash, err := identity.TokenDigest(token, FlowPrefix)
	if err != nil {
		return CallbackResult{}, ErrCredentials
	}
	flow, err := s.queries.FindFlow(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return CallbackResult{}, ErrCredentials
	}
	if err != nil {
		return CallbackResult{}, err
	}
	if FlowStatus(flow.Status) != FlowPending || len(code) == 0 || len(code) > maxOAuthCodeBytes || len(state) > maxOAuthStateBytes {
		return CallbackResult{}, ErrCredentials
	}
	if subtle.ConstantTimeCompare(identity.Digest(state), flow.StateHash) != 1 {
		return CallbackResult{}, ErrCredentials
	}
	if s.deps.Federation.Version(flow.ProviderID) != flow.ConfigVersion {
		return CallbackResult{}, ErrCredentials
	}
	if err := s.queries.claimPendingFlow(ctx, flow.ID); err != nil {
		return CallbackResult{}, err
	}
	completed := false
	defer func() {
		if !completed {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancel()
			_ = s.queries.FailFlow(cleanup, flow.ID)
		}
	}()
	var protocol protocolState
	if json.Unmarshal(flow.ProtocolState, &protocol) != nil {
		return CallbackResult{}, ErrCredentials
	}
	verified, err := s.deps.Federation.Verify(ctx, flow.ProviderID, flow.ConfigVersion, code, protocol.Verifier)
	if err != nil {
		return CallbackResult{}, err
	}
	if verified.Subject == "" || len(verified.Subject) > maxProviderSubjectBytes || verified.Namespace == "" || len(verified.Namespace) > maxProviderNamespaceBytes {
		return CallbackResult{}, ErrCredentials
	}
	if !utf8.ValidString(verified.Name) {
		verified.Name = ""
	}
	name := []rune(strings.TrimSpace(verified.Name))
	if len(name) > maxDisplayNameRunes {
		name = name[:maxDisplayNameRunes]
	}
	verified.Name = string(name)
	var result CallbackResult
	switch FlowPurpose(flow.Purpose) {
	case FlowLogin, FlowRegister:
		result, err = s.completeLogin(ctx, r, flow, verified)
	case FlowReauthenticate, FlowLinkIdentity:
		result, err = s.completeBoundFlow(ctx, r, flow, verified)
	default:
		err = ErrCredentials
	}
	completed = err == nil
	return result, err
}

// completeBoundFlow 在同一事务内复核原会话，并完成已绑定用户的证明。
func (s *Service) completeBoundFlow(ctx context.Context, r Request, flow sqlc.AuthFlow, verified VerifiedIdentity) (CallbackResult, error) {
	var result CallbackResult
	err := db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		if flow.UserID == nil || flow.SessionID == nil {
			return ErrCredentials
		}
		bound := Request{
			Subject: authorization.Subject{
				UserID:    flow.UserID.String(),
				SessionID: flow.SessionID.String(),
			},
			RequestID: r.RequestID,
		}
		user, _, err := s.readSelf(ctx, q, bound)
		if errors.Is(err, apperror.ErrUnauthenticated) {
			return ErrFlow
		}
		if err != nil {
			return err
		}
		if user.AuthVersion != flow.AuthVersion {
			return ErrCredentials
		}
		switch FlowPurpose(flow.Purpose) {
		case FlowReauthenticate:
			result, err = s.completeReauthentication(ctx, q, user, flow, verified)
		case FlowLinkIdentity:
			result, err = s.completeLinkProof(ctx, q, bound, flow, verified)
		default:
			err = ErrCredentials
		}
		return err
	})
	return result, err
}

func (s *Service) completeReauthentication(ctx context.Context, q *store, user sqlc.User, flow sqlc.AuthFlow, verified VerifiedIdentity) (CallbackResult, error) {
	if flow.AccountID == nil {
		return CallbackResult{}, ErrCredentials
	}
	account, err := q.GetAccount(ctx, sqlc.GetAccountParams{ID: *flow.AccountID, UserID: user.ID})
	if err != nil {
		return CallbackResult{}, credentialLookupError(err)
	}
	if account.Version != flow.AccountVersion || account.ProviderNamespace != verified.Namespace || account.ProviderAccountID != verified.Subject || !s.accountEnabled(account) {
		return CallbackResult{}, ErrCredentials
	}
	if err := q.verifyFlow(ctx, flow.ID, FlowAuthorized, verified); err != nil {
		return CallbackResult{}, err
	}
	return reauthenticatedCallback(flow.ID), nil
}

func (s *Service) completeLinkProof(ctx context.Context, q *store, bound Request, flow sqlc.AuthFlow, verified VerifiedIdentity) (CallbackResult, error) {
	if flow.ReauthenticationID == nil {
		return CallbackResult{}, ErrCredentials
	}
	if _, err := s.authorization(ctx, q, bound, *flow.ReauthenticationID, OperationLinkAccount, flow.ProviderID, &flow.ID); err != nil {
		return CallbackResult{}, err
	}
	if err := q.verifyFlow(ctx, flow.ID, FlowVerified, verified); err != nil {
		return CallbackResult{}, err
	}
	return linkPendingCallback(LinkConfirmation{FlowID: flow.ID, Provider: flow.ProviderID, Name: verified.Name}), nil
}

func (s *Service) completeLogin(ctx context.Context, r Request, flow sqlc.AuthFlow, verified VerifiedIdentity) (CallbackResult, error) {
	account, findErr := s.queries.FindProviderAccount(ctx, sqlc.FindProviderAccountParams{ProviderNamespace: verified.Namespace, ProviderAccountID: verified.Subject})
	if findErr != nil && !errors.Is(findErr, pgx.ErrNoRows) {
		return CallbackResult{}, findErr
	}
	if errors.Is(findErr, pgx.ErrNoRows) && FlowPurpose(flow.Purpose) != FlowRegister {
		return CallbackResult{}, ErrRegistrationRequired
	}
	var result CallbackResult
	err := db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		var user sqlc.User
		var err error
		if findErr == nil {
			user, err = q.LockUser(ctx, account.UserID)
			if err != nil {
				return err
			}
			current, err := q.GetAccount(ctx, sqlc.GetAccountParams{ID: account.ID, UserID: account.UserID})
			if err != nil {
				return credentialLookupError(err)
			}
			if current.Version != account.Version || UserStatus(user.Status) != UserActive || !s.accountEnabled(current) {
				return ErrCredentials
			}
			account = current
		} else {
			user, err = q.CreateFederatedUser(ctx, verified.Name)
			if err != nil {
				return err
			}
			account, err = q.CreateAccount(ctx, sqlc.CreateAccountParams{
				UserID:            user.ID,
				ProviderID:        flow.ProviderID,
				ProviderNamespace: verified.Namespace,
				ProviderAccountID: verified.Subject,
			})
			if err != nil {
				return err
			}
			registrationRequest := Request{
				Subject:   authorization.Subject{UserID: user.ID.String()},
				RequestID: r.RequestID,
			}
			if err = audit(ctx, q, registrationRequest, auditEvent{
				Action:       "auth.register",
				Outcome:      AuditSuccess,
				ResourceType: "user",
				ResourceID:   user.ID.String(),
				ScopeSubject: user.ID.String(),
			}); err != nil {
				return err
			}
		}
		if err := q.consumeFlow(ctx, flow.ID, FlowProcessing); err != nil {
			return err
		}
		session, err := s.newSession(ctx, q, r, user, account, MethodOAuth)
		result = sessionCallback(session)
		return err
	})
	return result, err
}
