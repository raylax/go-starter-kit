package identity

import (
	"context"
	"errors"

	"github.com/golang-jwt/jwt/v5"
)

// JWT 校验面向本 API 的签名访问令牌；不负责登录、签发令牌或校验不透明令牌。
type JWT struct {
	keyfunc  func(context.Context) jwt.Keyfunc
	issuer   string
	audience string
}

func (a *JWT) Authenticate(ctx context.Context, raw string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, a.keyfunc(ctx),
		jwt.WithValidMethods([]string{"RS256", "ES256"}),
		jwt.WithIssuer(a.issuer), jwt.WithAudience(a.audience), jwt.WithExpirationRequired(), jwt.WithIssuedAt(),
	)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, ErrUnavailable) {
		return "", err
	}
	if err != nil || ValidateSubject(claims.Subject) != nil {
		return "", ErrUnauthorized
	}
	return claims.Subject, nil
}
