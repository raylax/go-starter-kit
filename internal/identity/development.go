package identity

import (
	"context"
	"crypto/subtle"
)

// NewDevelopment 创建固定令牌认证器；允许启用的环境由应用配置校验。
func NewDevelopment(token, subject string) (Authenticator, error) {
	if err := ValidateDevelopment(token, subject); err != nil {
		return nil, err
	}
	return development{token: token, subject: subject}, nil
}

type development struct{ token, subject string }

func (a development) Authenticate(ctx context.Context, raw string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if a.token == "" || a.subject == "" || subtle.ConstantTimeCompare([]byte(raw), []byte(a.token)) != 1 {
		return "", ErrUnauthorized
	}
	return a.subject, nil
}
