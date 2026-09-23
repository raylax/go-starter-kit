// Package identity 校验令牌格式并传递已认证身份，不访问数据库或 HTTP。
package identity

import (
	"context"
	"errors"
)

var ErrUnauthorized = errors.New("invalid bearer token")
var ErrUnavailable = errors.New("authentication service unavailable")

// Authenticator 接收原始令牌，不解析 HTTP 请求头。
type Authenticator interface {
	Authenticate(context.Context, string) (Principal, error)
}

type Principal struct {
	Subject   string
	SessionID string
}
type principalKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}
func Current(ctx context.Context) Principal { p, _ := ctx.Value(principalKey{}).(Principal); return p }

func WithSubject(ctx context.Context, subject string) context.Context {
	return WithPrincipal(ctx, Principal{Subject: subject})
}

func Subject(ctx context.Context) string {
	return Current(ctx).Subject
}
