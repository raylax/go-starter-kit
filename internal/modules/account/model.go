// Package account 管理用户、登录账号、会话和认证流程。
package account

import (
	"time"

	"github.com/example/go-starter-kit/internal/apperror"
	"github.com/example/go-starter-kit/internal/authorization"
	"github.com/google/uuid"
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
	ID                uuid.UUID
	Method            AuthMethod
	AuthenticatedAt   time.Time
	CreatedAt         time.Time
	LastSeenAt        time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
}
type SessionCredentials struct {
	Token             string
	ID                uuid.UUID
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
}
type FlowResult struct {
	ID               uuid.UUID
	Token            string
	AuthorizationURL string
	ExpiresAt        time.Time
}

// CallbackResult 仅由 OAuth 回调产生，载荷通过对应结果的读取方法访问。
// 零值不是成功结果，HTTP 映射会将其作为内部错误处理。
type CallbackResult struct {
	result             AuthResult
	session            SessionCredentials
	reauthenticationID uuid.UUID
	link               LinkConfirmation
}

// LinkConfirmation 表示待用户确认的已验证第三方账号。
type LinkConfirmation struct {
	FlowID         uuid.UUID
	Provider, Name string
}

// ReauthenticationResult 只表示跳转继续认证或已经取得操作证明。
type ReauthenticationResult struct {
	result             AuthResult
	flow               FlowResult
	reauthenticationID uuid.UUID
}

type VerifiedIdentity struct {
	Namespace, Subject, Name string
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
