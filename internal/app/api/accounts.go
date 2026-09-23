package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/example/go-starter-kit/internal/authorization"
	"net/url"
	"os"
	"strings"

	"github.com/example/go-starter-kit/internal/modules/account"
	"github.com/example/go-starter-kit/internal/platform/federation"
	"github.com/example/go-starter-kit/internal/platform/password"
)

func newAccounts(cfg Config, database account.Database, authorizer authorization.Authorizer) (*account.Service, error) {
	hasher, err := password.New(4)
	if err != nil {
		return nil, err
	}
	deps := account.Dependencies{Authorizer: authorizer, Hash: hasher.Hash, Verify: hasher.Verify, ValidPassword: password.Validate}
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
			redirect, e := url.Parse(p.RedirectURI)
			if e != nil || redirect.Scheme+"://"+redirect.Host != cfg.FrontendURL || redirect.Path != "/auth/callback" || redirect.RawQuery != "" {
				return nil, fmt.Errorf("提供商回调必须是受信前端的 /auth/callback")
			}
		}
	}
	registry, e := federation.New(providers)
	if e != nil {
		return nil, e
	}
	deps.ProviderEnabled = registry.Enabled
	deps.ProviderVersion = registry.Version
	deps.StartProvider = func(ctx context.Context, id, state, verifier string) (string, string, error) {
		uri, version, e := registry.Start(ctx, id, state, verifier)
		return uri, version, mapProviderError(e)
	}
	deps.VerifyProvider = func(ctx context.Context, id, version, code, verifier string) (account.VerifiedIdentity, error) {
		v, e := registry.Verify(ctx, id, version, code, verifier)
		return account.VerifiedIdentity{Namespace: v.Namespace, Subject: v.Subject, Name: v.Name, AuthenticatedAt: v.AuthenticatedAt}, mapProviderError(e)
	}
	return account.NewService(database, account.Options{IdleTTL: cfg.AuthSessionIdleTTL, MaxTTL: cfg.AuthSessionMaxTTL, FrontendURL: cfg.FrontendURL}, deps)
}
func mapProviderError(e error) error {
	if errors.Is(e, federation.ErrProof) {
		return account.ErrCredentials
	}
	if errors.Is(e, federation.ErrUnavailable) {
		return account.ErrUnavailable
	}
	return e
}
