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

const passwordHashConcurrency = 4

func newAccounts(cfg Config, database account.Database, authorizer authorization.Authorizer, logger *slog.Logger) (*account.Service, error) {
	hasher, err := password.New(passwordHashConcurrency)
	if err != nil {
		return nil, err
	}
	deps := account.Dependencies{Authorizer: authorizer, Passwords: hasher, Logger: logger}
	var providers []federation.Config
	if cfg.AuthProvidersFile != "" {
		data, err := os.ReadFile(cfg.AuthProvidersFile)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &providers); err != nil {
			return nil, fmt.Errorf("第三方提供商配置文件无效")
		}
		for i := range providers {
			provider := &providers[i]
			secret, err := os.ReadFile(provider.ClientSecretFile)
			if err != nil {
				return nil, fmt.Errorf("读取提供商客户端密钥失败")
			}
			provider.ClientSecret = strings.TrimSpace(string(secret))
		}
	}
	registry, err := federation.New(providers)
	if err != nil {
		return nil, err
	}
	deps.Federation = federationAdapter{registry}
	return account.NewService(database, account.Options{
		IdleTTL:     cfg.AuthSessionIdleTTL,
		MaxTTL:      cfg.AuthSessionMaxTTL,
		FrontendURL: cfg.FrontendURL,
	}, deps)
}

func mapProviderError(err error) error {
	if errors.Is(err, federation.ErrProof) {
		return account.ErrCredentials
	}
	if errors.Is(err, federation.ErrUnavailable) {
		return fmt.Errorf("%w: %w", account.ErrUnavailable, err)
	}
	return err
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
	verified, err := a.registry.Verify(ctx, id, version, code, verifier)
	return account.VerifiedIdentity{Namespace: verified.Namespace, Subject: verified.Subject, Name: verified.Name}, mapProviderError(err)
}
