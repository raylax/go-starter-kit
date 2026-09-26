package account

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/example/go-starter-kit/internal/authorization"
	"github.com/example/go-starter-kit/internal/httpapi"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/example/go-starter-kit/internal/pagination"
	"github.com/google/uuid"
)

const maxRequestBodyBytes = 16 << 10 // 16 KiB

// operation 为账户路由补充统一的错误契约、请求上限和缓存策略。
func operation(policy httpapi.AuthPolicy, spec huma.Operation) httpapi.Operation {
	spec.Errors = []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusConflict,
		http.StatusRequestEntityTooLarge,
		http.StatusUnprocessableEntity,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	}
	spec.MaxBodyBytes = maxRequestBodyBytes
	op := httpapi.NewOperation(policy, spec)
	op.Middlewares = append(op.Middlewares, func(ctx huma.Context, next func(huma.Context)) {
		ctx.SetHeader("Cache-Control", "no-store")
		next(ctx)
	})
	return op
}

func request(ctx context.Context) Request {
	p := identity.Current(ctx)
	m := httpapi.Metadata(ctx)
	return Request{Subject: authorization.Subject{UserID: p.Subject, SessionID: p.SessionID}, RequestID: m.ID, ClientIP: m.ClientIP}
}

// Routes 提供认证和当前用户自助接口，管理接口由 AdminRoutes 单独注册。
func Routes() []httpapi.Route[*Service] {
	var routes []httpapi.Route[*Service]
	for _, group := range [][]httpapi.Route[*Service]{
		authenticationRoutes(), profileRoutes(), securityRoutes(),
		linkedAccountRoutes(), sessionRoutes(),
	} {
		routes = append(routes, group...)
	}
	return routes
}

// authenticationRoutes 注册、登录、恢复与一次性证明消费。
func authenticationRoutes() []httpapi.Route[*Service] {
	return []httpapi.Route[*Service]{
		httpapi.NoContentEndpoint(
			operation(httpapi.Public, huma.Operation{
				OperationID:   "register-user",
				Method:        http.MethodPost,
				Path:          "/v1/auth/register",
				Summary:       "发送注册验证邮件",
				Tags:          []string{"Authentication"},
				DefaultStatus: http.StatusAccepted,
			}),
			registerUser,
		),
		httpapi.MapEndpoint(
			operation(httpapi.Public, huma.Operation{
				OperationID: "login-user",
				Method:      http.MethodPost,
				Path:        "/v1/auth/login",
				Summary:     "使用密码登录",
				Tags:        []string{"Authentication"},
			}),
			loginUser,
			sessionOutput,
		),
		httpapi.MapEndpoint(
			operation(httpapi.Public, huma.Operation{
				OperationID: "start-oauth",
				Method:      http.MethodPost,
				Path:        "/v1/auth/oauth/{provider}",
				Summary:     "开始第三方登录或注册",
				Tags:        []string{"Authentication"},
			}),
			startOAuth,
			flowOutput,
		),
		httpapi.Endpoint(
			operation(httpapi.Public, huma.Operation{
				OperationID: "oauth-callback",
				Method:      http.MethodPost,
				Path:        "/v1/auth/oauth/callback",
				Summary:     "完成第三方证明校验",
				Tags:        []string{"Authentication"},
			}),
			oauthCallback,
		),
		httpapi.NoContentEndpoint(
			operation(httpapi.Public, huma.Operation{
				OperationID:   "forgot-password",
				Method:        http.MethodPost,
				Path:          "/v1/auth/password/forgot",
				Summary:       "发送密码恢复邮件",
				Tags:          []string{"Authentication"},
				DefaultStatus: http.StatusAccepted,
			}),
			forgotPassword,
		),
		httpapi.NoContentEndpoint(
			operation(httpapi.Public, huma.Operation{
				OperationID: "verify-user",
				Method:      http.MethodPost,
				Path:        "/v1/auth/verify",
				Summary:     "消费一次性验证挑战",
				Tags:        []string{"Authentication"},
			}),
			verifyUser,
		),
	}
}

// profileRoutes 当前用户的资料读取与修改。
func profileRoutes() []httpapi.Route[*Service] {
	return []httpapi.Route[*Service]{
		httpapi.ItemEndpoint(
			operation(httpapi.Session, huma.Operation{
				OperationID: "get-me",
				Method:      http.MethodGet,
				Path:        "/v1/me",
				Summary:     "获取当前用户与登录账号",
				Tags:        []string{"Profile"},
			}),
			getProfile,
			profileDTO,
		),
		httpapi.ItemEndpoint(
			operation(httpapi.Session, huma.Operation{
				OperationID: "update-me",
				Method:      http.MethodPatch,
				Path:        "/v1/me",
				Summary:     "修改展示资料",
				Tags:        []string{"Profile"},
			}),
			updateProfile,
			userDTO,
		),
	}
}

// securityRoutes 重新认证、密码与联系邮箱变更。
func securityRoutes() []httpapi.Route[*Service] {
	return []httpapi.Route[*Service]{
		httpapi.Endpoint(
			operation(httpapi.Session, huma.Operation{
				OperationID: "reauthenticate-user",
				Method:      http.MethodPost,
				Path:        "/v1/me/reauthenticate",
				Summary:     "为敏感操作重新认证",
				Tags:        []string{"Account Security"},
			}),
			reauthenticateUser,
		),
		httpapi.NoContentEndpoint(
			operation(httpapi.Session, huma.Operation{
				OperationID: "set-password",
				Method:      http.MethodPut,
				Path:        "/v1/me/password",
				Summary:     "新增或修改密码",
				Tags:        []string{"Account Security"},
			}),
			setPassword,
		),
		httpapi.NoContentEndpoint(
			operation(httpapi.Session, huma.Operation{
				OperationID:   "change-email",
				Method:        http.MethodPost,
				Path:          "/v1/me/email",
				Summary:       "发送联系邮箱变更验证",
				Tags:          []string{"Account Security"},
				DefaultStatus: http.StatusAccepted,
			}),
			changeEmail,
		),
	}
}

// linkedAccountRoutes 登录账号的显式绑定、确认与解绑。
func linkedAccountRoutes() []httpapi.Route[*Service] {
	return []httpapi.Route[*Service]{
		httpapi.MapEndpoint(
			operation(httpapi.Session, huma.Operation{
				OperationID: "link-account",
				Method:      http.MethodPost,
				Path:        "/v1/me/accounts/link",
				Summary:     "开始绑定第三方账号",
				Tags:        []string{"Linked Accounts"},
			}),
			startAccountLink,
			flowOutput,
		),
		httpapi.NoContentEndpoint(
			operation(httpapi.Session, huma.Operation{
				OperationID: "confirm-account-link",
				Method:      http.MethodPost,
				Path:        "/v1/me/accounts/link/confirm",
				Summary:     "确认绑定账号",
				Tags:        []string{"Linked Accounts"},
			}),
			confirmAccountLink,
		),
		httpapi.NoContentEndpoint(
			operation(httpapi.Session, huma.Operation{
				OperationID: "unlink-account",
				Method:      http.MethodPost,
				Path:        "/v1/me/accounts/{id}/unlink",
				Summary:     "解除登录账号绑定",
				Tags:        []string{"Linked Accounts"},
			}),
			unlinkAccount,
		),
	}
}

// sessionRoutes 当前用户的会话查询与撤销。
func sessionRoutes() []httpapi.Route[*Service] {
	return []httpapi.Route[*Service]{
		httpapi.PageEndpoint[SessionListResponse](
			operation(httpapi.Session, huma.Operation{
				OperationID: "list-sessions",
				Method:      http.MethodGet,
				Path:        "/v1/me/sessions",
				Summary:     "列出自己的会话",
				Tags:        []string{"Sessions"},
			}),
			listSessions,
			sessionRecordDTO,
		),
		httpapi.NoContentEndpoint(
			operation(httpapi.Session, huma.Operation{
				OperationID: "revoke-session",
				Method:      http.MethodDelete,
				Path:        "/v1/me/sessions/{id}",
				Summary:     "撤销指定会话或退出当前设备",
				Tags:        []string{"Sessions"},
			}),
			revokeSession,
		),
		httpapi.NoContentEndpoint(
			operation(httpapi.Session, huma.Operation{
				OperationID: "revoke-my-sessions",
				Method:      http.MethodDelete,
				Path:        "/v1/me/sessions",
				Summary:     "退出全部设备",
				Tags:        []string{"Sessions"},
			}),
			revokeMySessions,
		),
	}
}

func registerUser(service *Service, ctx context.Context, input *RegisterInput) error {
	return service.Register(ctx, request(ctx), input.Body.Email)
}

func loginUser(service *Service, ctx context.Context, input *LoginInput) (SessionCredentials, error) {
	return service.Login(ctx, request(ctx), input.Body.Email, input.Body.Password)
}

func startOAuth(service *Service, ctx context.Context, input *OAuthStartInput) (FlowResult, error) {
	return service.StartLogin(ctx, request(ctx), input.Provider, input.Body.Purpose)
}

func oauthCallback(service *Service, ctx context.Context, input *CallbackInput) (*CallbackOutput, error) {
	result, err := service.Callback(ctx, request(ctx), input.Body.Token, input.Body.Code, input.Body.State)
	if err != nil {
		return nil, err
	}
	return callbackOutput(result)
}

func forgotPassword(service *Service, ctx context.Context, input *ForgotInput) error {
	return service.ForgotPassword(ctx, request(ctx), input.Body.Email)
}

func verifyUser(service *Service, ctx context.Context, input *VerifyInput) error {
	return service.VerifyChallenge(ctx, request(ctx), input.Body.Token, Verification{
		Purpose:     input.Body.Purpose,
		NewPassword: input.Body.NewPassword,
	})
}

func getProfile(service *Service, ctx context.Context, _ *struct{}) (Profile, error) {
	return service.Me(ctx, request(ctx))
}

func updateProfile(service *Service, ctx context.Context, input *UpdateInput) (UserRecord, error) {
	return service.UpdateProfile(ctx, request(ctx), input.Body.DisplayName)
}

func reauthenticateUser(service *Service, ctx context.Context, input *ReauthenticateInput) (*ReauthenticateOutput, error) {
	result, err := service.Reauthenticate(ctx, request(ctx), Reauthentication{
		Method:    input.Body.Method,
		Password:  input.Body.Password,
		AccountID: input.Body.AccountID,
		Operation: input.Body.Operation,
		Target:    input.Body.Target,
	})
	if err != nil {
		return nil, err
	}
	return reauthenticateOutput(result)
}

func setPassword(service *Service, ctx context.Context, input *PasswordInput) error {
	id := uuid.Nil
	if input.Body.ReauthenticationID != nil {
		id = *input.Body.ReauthenticationID
	}
	return service.SetPassword(ctx, request(ctx), input.Body.CurrentPassword, input.Body.NewPassword, id)
}

func changeEmail(service *Service, ctx context.Context, input *EmailInput) error {
	return service.ChangeEmail(ctx, request(ctx), input.Body.Email, input.Body.ReauthenticationID)
}

func startAccountLink(service *Service, ctx context.Context, input *LinkInput) (FlowResult, error) {
	return service.StartLink(ctx, request(ctx), input.Body.Provider, input.Body.ReauthenticationID)
}

func confirmAccountLink(service *Service, ctx context.Context, input *ConfirmLinkInput) error {
	return service.ConfirmLink(ctx, request(ctx), input.Body.FlowID)
}

func unlinkAccount(service *Service, ctx context.Context, input *UnlinkInput) error {
	return service.Unlink(ctx, request(ctx), input.ID, input.Body.ReauthenticationID)
}

func listSessions(service *Service, ctx context.Context, input *ListInput) (pagination.Result[SessionRecord], error) {
	return service.Sessions(ctx, request(ctx), input.Params())
}

func revokeSession(service *Service, ctx context.Context, input *IDInput) error {
	return service.RevokeSession(ctx, request(ctx), input.ID, false)
}

func revokeMySessions(service *Service, ctx context.Context, _ *struct{}) error {
	return service.RevokeSession(ctx, request(ctx), uuid.Nil, true)
}
