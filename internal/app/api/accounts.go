package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/example/go-starter-kit/internal/authorization"
	"github.com/example/go-starter-kit/internal/modules/account"
	"github.com/example/go-starter-kit/internal/platform/federation"
	"github.com/example/go-starter-kit/internal/platform/password"
)

// newAuthorizer 将账户权限查询适配为各业务模块共用的授权接口。
func newAuthorizer(database account.Database) authorization.Authorizer {
	checker := account.NewAdminChecker(database)
	return authorization.AdminCheckFunc(checker.CheckAdmin)
}

func newAccounts(cfg Config, database account.Database, authorizer authorization.Authorizer, logger *slog.Logger) (*account.Service, error) {
	hasher, err := password.New(4)
	if err != nil {
		return nil, err
	}
	deps := account.Dependencies{Authorizer: authorizer, Passwords: hasher, Logger: logger}
	var providers []federation.Config
	if cfg.AuthProvidersFile != "" {
		data, e := os.ReadFile(cfg.AuthProvidersFile)
		if e != nil {
			return nil, e
		}
		if e = json.Unmarshal(data, &providers); e != nil {
			return nil, fmt.Errorf("第三方提供商配置文件无效")
		}
		for i := range providers {
			p := &providers[i]
			secret, e := os.ReadFile(p.ClientSecretFile)
			if e != nil {
				return nil, fmt.Errorf("读取提供商客户端密钥失败")
			}
			p.ClientSecret = strings.TrimSpace(string(secret))
		}
	}
	registry, e := federation.New(providers)
	if e != nil {
		return nil, e
	}
	deps.Federation = federationAdapter{registry}
	return account.NewService(database, account.Options{IdleTTL: cfg.AuthSessionIdleTTL, MaxTTL: cfg.AuthSessionMaxTTL, FrontendURL: cfg.FrontendURL}, deps)
}
func mapProviderError(e error) error {
	if errors.Is(e, federation.ErrProof) {
		return account.ErrCredentials
	}
	if errors.Is(e, federation.ErrUnavailable) {
		return fmt.Errorf("%w: %w", account.ErrUnavailable, e)
	}
	return e
}

// federationAdapter 将协议错误和身份记录转换为账户模块的契约。
type federationAdapter struct{ registry *federation.Registry }

func (a federationAdapter) Enabled(id string) bool   { return a.registry.Enabled(id) }
func (a federationAdapter) Version(id string) string { return a.registry.Version(id) }
func (a federationAdapter) Start(ctx context.Context, id, state, verifier string) (string, string, error) {
	uri, version, err := a.registry.Start(ctx, id, state, verifier)
	return uri, version, mapProviderError(err)
}
func (a federationAdapter) Verify(ctx context.Context, id, version, code, verifier string) (account.VerifiedIdentity, error) {
	v, err := a.registry.Verify(ctx, id, version, code, verifier)
	return account.VerifiedIdentity{Namespace: v.Namespace, Subject: v.Subject, Name: v.Name, AuthenticatedAt: v.AuthenticatedAt}, mapProviderError(err)
}
