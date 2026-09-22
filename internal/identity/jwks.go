package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/MicahParks/jwkset"
	"github.com/MicahParks/keyfunc/v3"
)

type refreshFailureKey struct{}
type refreshFailure struct{ err error }

// keyStorage 在 keyfunc 包装令牌错误之前保留依赖故障，
// 区分刷新限流与未知密钥标识。
type keyStorage struct{ jwkset.Storage }

func (s keyStorage) KeyRead(ctx context.Context, kid string) (jwkset.JWK, error) {
	failure := &refreshFailure{}
	key, err := s.Storage.KeyRead(context.WithValue(ctx, refreshFailureKey{}, failure), kid)
	if ctx.Err() != nil {
		return jwkset.JWK{}, ctx.Err()
	}
	if failure.err != nil || (err != nil && !errors.Is(err, jwkset.ErrKeyNotFound)) {
		return jwkset.JWK{}, ErrUnavailable
	}
	return key, err
}

// JWTConfig 只包含令牌校验需要的配置。
type JWTConfig struct{ Issuer, Audience, JWKSURL string }

func NewJWT(ctx context.Context, cfg JWTConfig) (*JWT, error) {
	// 上游客户端会将刷新故障归为未知密钥，因此使用本次查询的上下文保留原因，
	// 避免共享状态在并发查询之间串扰。
	allowInitialFailure := false
	keys, err := keyfunc.NewDefaultOverrideCtx(ctx, []string{cfg.JWKSURL}, keyfunc.Override{
		NoErrorReturnFirstHTTPReq: &allowInitialFailure,
		RefreshErrorHandlerFunc: func(string) func(context.Context, error) {
			return func(ctx context.Context, err error) {
				if failure, ok := ctx.Value(refreshFailureKey{}).(*refreshFailure); ok {
					failure.err = err
					return
				}
				if ctx.Err() == nil {
					slog.ErrorContext(ctx, "JWKS refresh failed")
				}
			}
		},
	})
	if err != nil {
		return nil, fmt.Errorf("initialize JWKS: %w", err)
	}
	verifier, err := keyfunc.New(keyfunc.Options{Storage: keyStorage{keys.Storage()}})
	if err != nil {
		return nil, fmt.Errorf("initialize JWT verifier: %w", err)
	}
	return &JWT{keyfunc: verifier.KeyfuncCtx, issuer: cfg.Issuer, audience: cfg.Audience}, nil
}
