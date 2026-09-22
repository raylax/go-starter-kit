// Package identity 校验访问令牌并传递已认证的用户标识。
package identity

import (
	"context"
	"errors"
)

var ErrUnauthorized = errors.New("invalid bearer token")
var ErrUnavailable = errors.New("authentication service unavailable")

// Authenticator 接收原始令牌，不解析 HTTP 请求头。
type Authenticator interface {
	Authenticate(context.Context, string) (string, error)
}

type subjectKey struct{}

func WithSubject(ctx context.Context, subject string) context.Context {
	return context.WithValue(ctx, subjectKey{}, subject)
}

func Subject(ctx context.Context) string {
	subject, _ := ctx.Value(subjectKey{}).(string)
	return subject
}
