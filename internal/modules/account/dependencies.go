package account

import (
	"context"
	"log/slog"

	"github.com/example/go-starter-kit/internal/authorization"
)

// Passwords 隔离密码校验与哈希实现。
type Passwords interface {
	Hash(context.Context, string) (string, error)
	Verify(context.Context, string, string) (bool, bool, error)
	Validate(string) bool
}

// Federation 的实现必须同时提供配置查询、发起和证明校验能力。
type Federation interface {
	Enabled(string) bool
	Version(string) string
	Start(context.Context, string, string, string) (string, string, error)
	Verify(context.Context, string, string, string, string) (VerifiedIdentity, error)
}

// Dependencies 汇集账户服务的外部能力，数据库事务由本模块掌握。
type Dependencies struct {
	Authorizer authorization.Authorizer
	Passwords  Passwords
	Federation Federation
	Logger     *slog.Logger
}

// disabledFederation 明确表示整个第三方认证能力未启用。
type disabledFederation struct{}

func (disabledFederation) Enabled(string) bool   { return false }
func (disabledFederation) Version(string) string { return "" }
func (disabledFederation) Start(context.Context, string, string, string) (string, string, error) {
	return "", "", ErrUnavailable
}
func (disabledFederation) Verify(context.Context, string, string, string, string) (VerifiedIdentity, error) {
	return VerifiedIdentity{}, ErrUnavailable
}
