package account

import (
	"errors"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/example/go-starter-kit/internal/httpapi"
)

type User struct {
	ID            string     `json:"id" minLength:"1" maxLength:"255"`
	DisplayName   string     `json:"display_name"`
	Email         *string    `json:"email"`
	EmailVerified bool       `json:"email_verified"`
	Status        UserStatus `json:"status" enum:"pending,active,disabled"`
	Role          UserRole   `json:"role" enum:"user,admin"`
	CreatedAt     time.Time  `json:"created_at"`
}
type Account struct {
	ID         string     `json:"id" minLength:"1" maxLength:"255"`
	Provider   string     `json:"provider"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	Enabled    bool       `json:"enabled"`
}
type UserProfile struct {
	User      User      `json:"user"`
	Accounts  []Account `json:"accounts"`
	SessionID string    `json:"session_id" minLength:"1" maxLength:"255"`
}
type Session struct {
	ID                string     `json:"id" minLength:"1" maxLength:"255"`
	Method            AuthMethod `json:"method" enum:"password,oauth"`
	AuthenticatedAt   time.Time  `json:"authenticated_at"`
	CreatedAt         time.Time  `json:"created_at"`
	LastSeenAt        time.Time  `json:"last_seen_at"`
	IdleExpiresAt     time.Time  `json:"idle_expires_at"`
	AbsoluteExpiresAt time.Time  `json:"absolute_expires_at"`
}
type SessionCreateResponse struct {
	Token             string    `json:"token"`
	TokenType         string    `json:"token_type" enum:"Bearer"`
	SessionID         string    `json:"session_id" minLength:"1" maxLength:"255"`
	IdleExpiresAt     time.Time `json:"idle_expires_at"`
	AbsoluteExpiresAt time.Time `json:"absolute_expires_at"`
}
type AuthFlowResponse struct {
	FlowID           string    `json:"flow_id" minLength:"1" maxLength:"255"`
	Token            string    `json:"token"`
	AuthorizationURL string    `json:"authorization_url"`
	ExpiresAt        time.Time `json:"expires_at"`
}
type AuthSessionResult struct {
	Result  AuthResult            `json:"result" enum:"session"`
	Session SessionCreateResponse `json:"session"`
}
type AuthReauthenticatedResult struct {
	Result             AuthResult `json:"result" enum:"reauthenticated"`
	ReauthenticationID string     `json:"reauthentication_id" minLength:"1" maxLength:"255"`
}
type AuthLinkPendingResult struct {
	Result   AuthResult `json:"result" enum:"link_pending"`
	FlowID   string     `json:"flow_id" minLength:"1" maxLength:"255"`
	Provider string     `json:"provider"`
	Name     string     `json:"name"`
}
type AuthRedirectResult struct {
	Result AuthResult       `json:"result" enum:"redirect"`
	Flow   AuthFlowResponse `json:"flow"`
}
type AuthCallbackResponse struct {
	Result             AuthResult             `json:"result"`
	Session            *SessionCreateResponse `json:"session,omitempty"`
	ReauthenticationID *string                `json:"reauthentication_id,omitempty" minLength:"1" maxLength:"255"`
	FlowID             *string                `json:"flow_id,omitempty" minLength:"1" maxLength:"255"`
	Provider           *string                `json:"provider,omitempty"`
	Name               *string                `json:"name,omitempty"`
}
type UserReauthenticateResponse struct {
	Result             AuthResult        `json:"result"`
	ReauthenticationID *string           `json:"reauthentication_id,omitempty" minLength:"1" maxLength:"255"`
	Flow               *AuthFlowResponse `json:"flow,omitempty"`
}

func oneOf(r huma.Registry, values ...any) *huma.Schema {
	refs := make([]*huma.Schema, 0, len(values))
	for _, v := range values {
		refs = append(refs, r.Schema(reflect.TypeOf(v), true, ""))
	}
	return &huma.Schema{OneOf: refs}
}
func (AuthCallbackResponse) Schema(r huma.Registry) *huma.Schema {
	return oneOf(r, AuthSessionResult{}, AuthReauthenticatedResult{}, AuthLinkPendingResult{})
}
func (UserReauthenticateResponse) Schema(r huma.Registry) *huma.Schema {
	return oneOf(r, AuthRedirectResult{}, AuthReauthenticatedResult{})
}

type UserRegisterRequest struct {
	Email string `json:"email" format:"email" maxLength:"254"`
}
type UserLoginRequest struct {
	Email    string `json:"email" format:"email" maxLength:"254"`
	Password string `json:"password" minLength:"1" maxLength:"128"`
}
type UserPasswordForgotRequest UserRegisterRequest
type AuthOAuthStartRequest struct {
	Purpose FlowPurpose `json:"purpose" enum:"login,register"`
}
type AuthOAuthCallbackRequest struct {
	Token string `json:"token" minLength:"1" maxLength:"128"`
	Code  string `json:"code" minLength:"1" maxLength:"8192"`
	State string `json:"state" minLength:"1" maxLength:"128"`
}
type UserRegistrationVerifyRequest struct {
	Token       string              `json:"token" minLength:"1" maxLength:"128"`
	Purpose     VerificationPurpose `json:"purpose" enum:"register"`
	NewPassword string              `json:"new_password" minLength:"15" maxLength:"128"`
}
type UserPasswordResetVerifyRequest struct {
	Token       string              `json:"token" minLength:"1" maxLength:"128"`
	Purpose     VerificationPurpose `json:"purpose" enum:"reset_password"`
	NewPassword string              `json:"new_password" minLength:"15" maxLength:"128"`
}
type UserEmailVerifyRequest struct {
	Token   string              `json:"token" minLength:"1" maxLength:"128"`
	Purpose VerificationPurpose `json:"purpose" enum:"change_email"`
}
type UserVerifyRequest struct {
	Token       string              `json:"token" minLength:"1" maxLength:"128"`
	Purpose     VerificationPurpose `json:"purpose"`
	NewPassword string              `json:"new_password,omitempty"`
}

func (UserVerifyRequest) Schema(r huma.Registry) *huma.Schema {
	return oneOf(r, UserRegistrationVerifyRequest{}, UserPasswordResetVerifyRequest{}, UserEmailVerifyRequest{})
}

type UserUpdateRequest struct {
	DisplayName string `json:"display_name" minLength:"1" maxLength:"100"`
}
type UserPasswordReauthenticateRequest struct {
	Method    AuthMethod `json:"method" enum:"password"`
	Password  string     `json:"password" minLength:"1" maxLength:"128"`
	Operation Operation  `json:"operation" enum:"link_account,unlink_account,set_password,change_email"`
	Target    string     `json:"target" minLength:"1" maxLength:"255"`
}
type UserOAuthReauthenticateRequest struct {
	Method    AuthMethod `json:"method" enum:"oauth"`
	AccountID string     `json:"account_id" minLength:"1" maxLength:"255"`
	Operation Operation  `json:"operation" enum:"link_account,unlink_account,set_password,change_email"`
	Target    string     `json:"target" minLength:"1" maxLength:"255"`
}
type UserReauthenticateRequest struct {
	Method    AuthMethod `json:"method" enum:"password,oauth"`
	Password  string     `json:"password,omitempty"`
	AccountID string     `json:"account_id,omitempty" minLength:"1" maxLength:"255"`
	Operation Operation  `json:"operation"`
	Target    string     `json:"target"`
}

func (UserReauthenticateRequest) Schema(r huma.Registry) *huma.Schema {
	return oneOf(r, UserPasswordReauthenticateRequest{}, UserOAuthReauthenticateRequest{})
}

type AccountLinkRequest struct {
	Provider           string `json:"provider" minLength:"1" maxLength:"64"`
	ReauthenticationID string `json:"reauthentication_id" minLength:"1" maxLength:"255"`
}
type AccountLinkConfirmRequest struct {
	FlowID string `json:"flow_id" minLength:"1" maxLength:"255"`
}
type AccountUnlinkRequest struct {
	ReauthenticationID string `json:"reauthentication_id" minLength:"1" maxLength:"255"`
}
type UserPasswordSetRequest struct {
	NewPassword        string  `json:"new_password" minLength:"15" maxLength:"128"`
	CurrentPassword    string  `json:"current_password,omitempty" maxLength:"128"`
	ReauthenticationID *string `json:"reauthentication_id,omitempty" minLength:"1" maxLength:"255"`
}
type UserEmailChangeRequest struct {
	Email              string `json:"email" format:"email" maxLength:"254"`
	ReauthenticationID string `json:"reauthentication_id" minLength:"1" maxLength:"255"`
}

type RegisterInput struct{ Body UserRegisterRequest }
type LoginInput struct{ Body UserLoginRequest }
type ForgotInput struct{ Body UserPasswordForgotRequest }
type OAuthStartInput struct {
	Provider string `path:"provider" maxLength:"64"`
	Body     AuthOAuthStartRequest
}
type CallbackInput struct{ Body AuthOAuthCallbackRequest }
type VerifyInput struct{ Body UserVerifyRequest }
type UpdateInput struct{ Body UserUpdateRequest }
type ReauthenticateInput struct{ Body UserReauthenticateRequest }
type LinkInput struct{ Body AccountLinkRequest }
type ConfirmLinkInput struct{ Body AccountLinkConfirmRequest }
type UnlinkInput struct {
	ID   string `path:"id" minLength:"1" maxLength:"255"`
	Body AccountUnlinkRequest
}
type PasswordInput struct{ Body UserPasswordSetRequest }
type EmailInput struct{ Body UserEmailChangeRequest }
type IDInput struct {
	ID string `path:"id" minLength:"1" maxLength:"255"`
}
type ListInput struct{ httpapi.PageQuery }
type SessionListResponse httpapi.Page[Session]
type SessionOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         SessionCreateResponse
}
type FlowOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         AuthFlowResponse
}
type CallbackOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         AuthCallbackResponse
}
type ReauthenticateOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         UserReauthenticateResponse
}

func sessionDTO(s SessionCredentials) SessionCreateResponse {
	return SessionCreateResponse{Token: s.Token, TokenType: "Bearer", SessionID: s.ID, IdleExpiresAt: s.IdleExpiresAt, AbsoluteExpiresAt: s.AbsoluteExpiresAt}
}
func flowDTO(f FlowResult) AuthFlowResponse {
	return AuthFlowResponse{FlowID: f.ID, Token: f.Token, AuthorizationURL: f.AuthorizationURL, ExpiresAt: f.ExpiresAt}
}
func profileDTO(p Profile) UserProfile {
	accounts := make([]Account, 0, len(p.Accounts))
	for _, a := range p.Accounts {
		accounts = append(accounts, Account(a))
	}
	return UserProfile{User: User(p.User), Accounts: accounts, SessionID: p.SessionID}
}

func sessionOutput(value SessionCredentials) *SessionOutput {
	return &SessionOutput{CacheControl: "no-store", Body: sessionDTO(value)}
}

func flowOutput(value FlowResult) *FlowOutput {
	return &FlowOutput{CacheControl: "no-store", Body: flowDTO(value)}
}

func userDTO(value UserRecord) User { return User(value) }

func sessionRecordDTO(value SessionRecord) Session { return Session(value) }

var errInvalidAuthenticationResult = errors.New("认证结果状态无效")

func callbackOutput(result CallbackResult) (*CallbackOutput, error) {
	body := AuthCallbackResponse{Result: result.result}
	switch result.result {
	case ResultSession:
		if result.session.ID == "" || result.session.Token == "" || result.session.IdleExpiresAt.IsZero() || result.session.AbsoluteExpiresAt.IsZero() {
			return nil, errInvalidAuthenticationResult
		}
		session := sessionDTO(result.session)
		body.Session = &session
	case ResultReauthenticated:
		if result.reauthenticationID == "" {
			return nil, errInvalidAuthenticationResult
		}
		body.ReauthenticationID = &result.reauthenticationID
	case ResultLinkPending:
		if result.link.FlowID == "" || result.link.Provider == "" {
			return nil, errInvalidAuthenticationResult
		}
		body.FlowID = &result.link.FlowID
		body.Provider = &result.link.Provider
		body.Name = &result.link.Name
	default:
		return nil, errInvalidAuthenticationResult
	}
	return &CallbackOutput{CacheControl: "no-store", Body: body}, nil
}

func reauthenticateOutput(result ReauthenticationResult) (*ReauthenticateOutput, error) {
	body := UserReauthenticateResponse{Result: result.result}
	switch result.result {
	case ResultRedirect:
		if result.flow.ID == "" || result.flow.Token == "" || result.flow.AuthorizationURL == "" || result.flow.ExpiresAt.IsZero() {
			return nil, errInvalidAuthenticationResult
		}
		flow := flowDTO(result.flow)
		body.Flow = &flow
	case ResultReauthenticated:
		if result.reauthenticationID == "" {
			return nil, errInvalidAuthenticationResult
		}
		body.ReauthenticationID = &result.reauthenticationID
	default:
		return nil, errInvalidAuthenticationResult
	}
	return &ReauthenticateOutput{CacheControl: "no-store", Body: body}, nil
}
