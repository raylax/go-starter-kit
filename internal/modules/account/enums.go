package account

// 枚举按业务语义分离，空值和未知值均无效；数据库边界显式转换。
type UserStatus string

const (
	UserPending  UserStatus = "pending"
	UserActive   UserStatus = "active"
	UserDisabled UserStatus = "disabled"
)

func (v UserStatus) Valid() bool {
	switch v {
	case UserPending, UserActive, UserDisabled:
		return true
	default:
		return false
	}
}

type UserRole string

const (
	RoleUser  UserRole = "user"
	RoleAdmin UserRole = "admin"
)

func (v UserRole) Valid() bool {
	switch v {
	case RoleUser, RoleAdmin:
		return true
	default:
		return false
	}
}

type AuthMethod string

const (
	MethodPassword AuthMethod = "password"
	MethodOAuth    AuthMethod = "oauth"
)

func (v AuthMethod) Valid() bool {
	switch v {
	case MethodPassword, MethodOAuth:
		return true
	default:
		return false
	}
}

type FlowPurpose string

const (
	FlowLogin          FlowPurpose = "login"
	FlowRegister       FlowPurpose = "register"
	FlowReauthenticate FlowPurpose = "reauthenticate"
	FlowLinkIdentity   FlowPurpose = "link_identity"
)

func (v FlowPurpose) Valid() bool {
	switch v {
	case FlowLogin, FlowRegister, FlowReauthenticate, FlowLinkIdentity:
		return true
	default:
		return false
	}
}

type FlowStatus string

const (
	FlowPending    FlowStatus = "pending"
	FlowProcessing FlowStatus = "processing"
	FlowVerified   FlowStatus = "verified"
	FlowAuthorized FlowStatus = "authorized"
	FlowClaimed    FlowStatus = "claimed"
	FlowConsumed   FlowStatus = "consumed"
	FlowFailed     FlowStatus = "failed"
)

func (v FlowStatus) Valid() bool {
	switch v {
	case FlowPending, FlowProcessing, FlowVerified, FlowAuthorized, FlowClaimed, FlowConsumed, FlowFailed:
		return true
	default:
		return false
	}
}

type VerificationPurpose string

const (
	VerifyRegister      VerificationPurpose = "register"
	VerifyResetPassword VerificationPurpose = "reset_password"
	VerifyChangeEmail   VerificationPurpose = "change_email"
)

func (v VerificationPurpose) Valid() bool {
	switch v {
	case VerifyRegister, VerifyResetPassword, VerifyChangeEmail:
		return true
	default:
		return false
	}
}

type Operation string

const (
	OperationLinkAccount   Operation = "link_account"
	OperationUnlinkAccount Operation = "unlink_account"
	OperationSetPassword   Operation = "set_password"
	OperationChangeEmail   Operation = "change_email"
)

func (v Operation) Valid() bool {
	switch v {
	case OperationLinkAccount, OperationUnlinkAccount, OperationSetPassword, OperationChangeEmail:
		return true
	default:
		return false
	}
}

type AuthResult string

const (
	ResultSession         AuthResult = "session"
	ResultRedirect        AuthResult = "redirect"
	ResultReauthenticated AuthResult = "reauthenticated"
	ResultLinkPending     AuthResult = "link_pending"
)

func (v AuthResult) Valid() bool {
	switch v {
	case ResultSession, ResultRedirect, ResultReauthenticated, ResultLinkPending:
		return true
	default:
		return false
	}
}

type AuditOutcome string

const (
	AuditSuccess AuditOutcome = "success"
	AuditFailure AuditOutcome = "failure"
	AuditDenied  AuditOutcome = "denied"
)

func (v AuditOutcome) Valid() bool {
	switch v {
	case AuditSuccess, AuditFailure, AuditDenied:
		return true
	default:
		return false
	}
}

type AuditActorType string

const (
	ActorUser      AuditActorType = "user"
	ActorSystem    AuditActorType = "system"
	ActorAnonymous AuditActorType = "anonymous"
)

func (v AuditActorType) Valid() bool {
	switch v {
	case ActorUser, ActorSystem, ActorAnonymous:
		return true
	default:
		return false
	}
}

// Settable 限制管理端可设置的用户状态，pending 仅用于注册流程。
func (v UserStatus) Settable() bool { return v == UserActive || v == UserDisabled }

// PublicStart 限制匿名入口只能启动登录或注册。
func (v FlowPurpose) PublicStart() bool { return v == FlowLogin || v == FlowRegister }

// Initial 限制新流程的初始状态。
func (v FlowStatus) Initial() bool { return v == FlowPending || v == FlowAuthorized }

// Verifiable 限制证明校验成功后的目标状态。
func (v FlowStatus) Verifiable() bool { return v == FlowVerified || v == FlowAuthorized }

// 内置提供商保留字不是第三方提供商的封闭枚举。
const CredentialProvider = "credential"
const LocalNamespace = "local"
const SecurityNotificationKind = "security_notification"
