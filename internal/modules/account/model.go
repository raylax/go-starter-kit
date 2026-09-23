// Package account 管理用户、登录账号、会话和认证流程。
package account

import (
	"context"
	"github.com/example/go-starter-kit/internal/apperror"
	"github.com/example/go-starter-kit/internal/authorization"
	"github.com/google/uuid"
	"time"
)

var (
	ErrVerification         = apperror.New(apperror.Invalid, "verification_invalid")
	ErrFlow                 = apperror.New(apperror.Invalid, "flow_invalid")
	ErrReauthentication     = apperror.New(apperror.Invalid, "reauthentication_invalid")
	ErrInvalid              = apperror.New(apperror.Invalid, "认证参数无效")
	ErrCredentials          = apperror.New(apperror.Unauthenticated, "登录凭据或验证证明无效")
	ErrNotFound             = apperror.New(apperror.NotFound, "资源不存在")
	ErrConflict             = apperror.New(apperror.Conflict, "账号或邮箱已被使用")
	ErrLastAccount          = apperror.New(apperror.Conflict, "必须保留并验证至少一个可用登录账号")
	ErrForbidden            = apperror.New(apperror.Forbidden, "无权执行此操作")
	ErrRateLimited          = apperror.New(apperror.RateLimited, "请求过于频繁，请稍后再试")
	ErrUnavailable          = apperror.New(apperror.Unavailable, "认证依赖暂不可用")
	ErrRegistrationRequired = apperror.New(apperror.Conflict, "registration_required")
)

const SessionPrefix = "tk_"
const FlowPrefix = "flow_"
const ChallengePrefix = "verify_"

// Request 将已认证主体与请求元数据组合，授权检查直接复用 Subject。
type Request struct {
	authorization.Subject
	RequestID, ClientIP string
}
type UserRecord struct {
	ID            uuid.UUID
	DisplayName   string
	Email         *string
	EmailVerified bool
	Status        UserStatus
	Role          UserRole
	CreatedAt     time.Time
}
type AccountRecord struct {
	ID         uuid.UUID
	Provider   string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	Enabled    bool
}
type Profile struct {
	User      UserRecord
	Accounts  []AccountRecord
	SessionID uuid.UUID
}
type SessionRecord struct {
	ID                                                                       uuid.UUID
	Method                                                                   AuthMethod
	AuthenticatedAt, CreatedAt, LastSeenAt, IdleExpiresAt, AbsoluteExpiresAt time.Time
}
type SessionCredentials struct {
	Token                            string
	ID                               uuid.UUID
	IdleExpiresAt, AbsoluteExpiresAt time.Time
}
type FlowResult struct {
	ID                      uuid.UUID
	Token, AuthorizationURL string
	ExpiresAt               time.Time
}
type AuthenticationResult struct {
	Result             AuthResult
	Session            SessionCredentials
	Flow               FlowResult
	ReauthenticationID uuid.UUID
	Provider, Name     string
}
type VerifiedIdentity struct {
	Namespace, Subject, Name string
	AuthenticatedAt          time.Time
}
type Reauthentication struct {
	Method           AuthMethod
	Operation        Operation
	Password, Target string
	AccountID        uuid.UUID
}
type Verification struct {
	Purpose     VerificationPurpose
	NewPassword string
}
type Options struct {
	IdleTTL, MaxTTL time.Duration
	FrontendURL     string
}

// Dependencies 隔离密码及第三方协议，数据库事务由本模块掌握。
type Dependencies struct {
	Authorizer      authorization.Authorizer
	Hash            func(context.Context, string) (string, error)
	Verify          func(context.Context, string, string) (bool, bool, error)
	ValidPassword   func(string) bool
	ProviderEnabled func(string) bool
	ProviderVersion func(string) string
	StartProvider   func(context.Context, string, string, string) (string, string, error)
	VerifyProvider  func(context.Context, string, string, string, string) (VerifiedIdentity, error)
}
