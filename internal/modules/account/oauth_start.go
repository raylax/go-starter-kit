package account

import (
	"context"
	"encoding/json"

	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// oauthFlowStart 只封装提供商协议材料，各业务入口在事务中补齐用途及归属。
type oauthFlowStart struct {
	params           sqlc.CreateFlowParams
	token            string
	authorizationURL string
}

func (s *Service) prepareOAuthFlow(ctx context.Context, provider string) (oauthFlowStart, error) {
	if !s.deps.Federation.Enabled(provider) {
		return oauthFlowStart{}, ErrInvalid
	}
	token, tokenHash := identity.NewToken(FlowPrefix)
	state, stateHash := identity.NewToken("")
	verifier, _ := identity.NewToken("")
	authorizationURL, version, err := s.deps.Federation.Start(ctx, provider, state, verifier)
	if err != nil {
		return oauthFlowStart{}, err
	}
	// 协议状态以 JSON 原文短期保存，流程完成或失败时清除。
	protocol, _ := json.Marshal(protocolState{Verifier: verifier})
	return oauthFlowStart{
		params: sqlc.CreateFlowParams{
			ID:            uuid.New(),
			TokenHash:     tokenHash,
			ProviderID:    provider,
			ConfigVersion: version,
			StateHash:     stateHash,
			ProtocolState: protocol,
			Status:        string(FlowPending),
		},
		token:            token,
		authorizationURL: authorizationURL,
	}, nil
}

func (start oauthFlowStart) persist(ctx context.Context, q *store, params sqlc.CreateFlowParams) (FlowResult, error) {
	row, err := q.CreateFlow(ctx, params)
	if err != nil {
		return FlowResult{}, err
	}
	return FlowResult{ID: row.ID, Token: start.token, AuthorizationURL: start.authorizationURL, ExpiresAt: row.ExpiresAt}, nil
}

func (s *Service) StartLogin(ctx context.Context, r Request, provider string, purpose FlowPurpose) (FlowResult, error) {
	if !purpose.PublicStart() {
		return FlowResult{}, ErrInvalid
	}
	if err := s.limit(ctx, "oauth.ip", r.ClientIP, ipRequestLimit, ipRateWindow); err != nil {
		return FlowResult{}, err
	}
	return s.startLoginFlow(ctx, provider, purpose)
}

func (s *Service) startLoginFlow(ctx context.Context, provider string, purpose FlowPurpose) (FlowResult, error) {
	start, err := s.prepareOAuthFlow(ctx, provider)
	if err != nil {
		return FlowResult{}, err
	}
	var result FlowResult
	err = db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		params := start.params
		params.Purpose = string(purpose)
		result, err = start.persist(ctx, newStore(tx), params)
		return err
	})
	return result, err
}

func (s *Service) StartLink(ctx context.Context, r Request, provider string, reauthID uuid.UUID) (FlowResult, error) {
	if err := s.entryLimit(ctx, r, "link", r.UserID); err != nil {
		return FlowResult{}, err
	}
	return s.startLinkFlow(ctx, r, provider, reauthID)
}

func (s *Service) startLinkFlow(ctx context.Context, r Request, provider string, reauthID uuid.UUID) (FlowResult, error) {
	start, err := s.prepareOAuthFlow(ctx, provider)
	if err != nil {
		return FlowResult{}, err
	}
	var result FlowResult
	err = db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		user, session, err := s.readSelf(ctx, q, r)
		if err != nil {
			return err
		}
		if _, err := s.authorization(ctx, q, r, reauthID, OperationLinkAccount, provider, nil); err != nil {
			return err
		}
		params := start.params
		params.Purpose = string(FlowLinkIdentity)
		params.UserID = &user.ID
		params.SessionID = &session.ID
		params.AuthVersion = user.AuthVersion
		params.Operation = string(OperationLinkAccount)
		params.Target = provider
		params.ReauthenticationID = &reauthID
		flow, err := start.persist(ctx, q, params)
		if err != nil {
			return err
		}
		// 先插入流程完成外键检查，再以条件更新认领；竞争失败时整体回滚。
		if err := q.claimAuthorization(ctx, reauthID, flow.ID); err != nil {
			return proofError(err, ErrReauthentication)
		}
		result = flow
		return nil
	})
	return result, err
}

func (s *Service) startReauthenticationFlow(ctx context.Context, r Request, account sqlc.Account, operation Operation, target string) (FlowResult, error) {
	start, err := s.prepareOAuthFlow(ctx, account.ProviderID)
	if err != nil {
		return FlowResult{}, err
	}
	var result FlowResult
	err = db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		user, session, err := s.readSelf(ctx, q, r)
		if err != nil {
			return err
		}
		current, err := q.GetAccount(ctx, sqlc.GetAccountParams{ID: account.ID, UserID: user.ID})
		if err != nil {
			return credentialLookupError(err)
		}
		if current.Version != account.Version || !s.accountEnabled(current) {
			return ErrCredentials
		}
		params := start.params
		params.Purpose = string(FlowReauthenticate)
		params.UserID = &user.ID
		params.SessionID = &session.ID
		params.AuthVersion = user.AuthVersion
		params.AccountID = &account.ID
		params.AccountVersion = account.Version
		params.Operation = string(operation)
		params.Target = target
		result, err = start.persist(ctx, q, params)
		return err
	})
	return result, err
}
