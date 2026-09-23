package account

import (
	"context"
	"github.com/danielgtaylor/huma/v2"
	"github.com/example/go-starter-kit/internal/authorization"
	"github.com/example/go-starter-kit/internal/httpapi"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/example/go-starter-kit/internal/pagination"
	"github.com/google/uuid"
	"net/http"
)

func operation(tag, id, method, path, summary string, policy httpapi.AuthPolicy) huma.Operation {
	op := huma.Operation{OperationID: id, Method: method, Path: path, Summary: summary, Tags: []string{tag}, Errors: []int{400, 401, 403, 404, 409, 413, 422, 429, 500, 503, 504}, MaxBodyBytes: 16 << 10}
	op.Middlewares = append(op.Middlewares, func(ctx huma.Context, next func(huma.Context)) {
		ctx.SetHeader("Cache-Control", "no-store")
		next(ctx)
	})
	if policy != httpapi.Public {
		op.Security = []map[string][]string{{string(policy): {}}}
	}
	return op
}
func accepted(id, path, summary string) huma.Operation {
	op := operation("Authentication", id, http.MethodPost, path, summary, httpapi.Public)
	op.DefaultStatus = 202
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
		httpapi.NoContentEndpoint(accepted("register-user", "/v1/auth/register", "发送注册验证邮件"), func(s *Service, c context.Context, i *RegisterInput) error {
			return s.Register(c, request(c), i.Body.Email)
		}),
		httpapi.MapEndpoint(operation("Authentication", "login-user", "POST", "/v1/auth/login", "使用密码登录", httpapi.Public), func(s *Service, c context.Context, i *LoginInput) (SessionCredentials, error) {
			return s.Login(c, request(c), i.Body.Email, i.Body.Password)
		}, func(v SessionCredentials) *SessionOutput {
			return &SessionOutput{CacheControl: "no-store", Body: sessionDTO(v)}
		}),
		httpapi.MapEndpoint(operation("Authentication", "start-oauth", "POST", "/v1/auth/oauth/{provider}", "开始第三方登录或注册", httpapi.Public), func(s *Service, c context.Context, i *OAuthStartInput) (FlowResult, error) {
			return s.StartLogin(c, request(c), i.Provider, i.Body.Purpose)
		}, func(v FlowResult) *FlowOutput { return &FlowOutput{CacheControl: "no-store", Body: flowDTO(v)} }),
		httpapi.MapEndpoint(operation("Authentication", "oauth-callback", "POST", "/v1/auth/oauth/callback", "完成第三方证明校验", httpapi.Public), func(s *Service, c context.Context, i *CallbackInput) (AuthenticationResult, error) {
			return s.Callback(c, request(c), i.Body.Token, i.Body.Code, i.Body.State)
		}, callbackOutput),
		httpapi.NoContentEndpoint(accepted("forgot-password", "/v1/auth/password/forgot", "发送密码恢复邮件"), func(s *Service, c context.Context, i *ForgotInput) error {
			return s.ForgotPassword(c, request(c), i.Body.Email)
		}),
		httpapi.NoContentEndpoint(operation("Authentication", "verify-user", "POST", "/v1/auth/verify", "消费一次性验证挑战", httpapi.Public), func(s *Service, c context.Context, i *VerifyInput) error {
			return s.VerifyChallenge(c, request(c), i.Body.Token, Verification{Purpose: i.Body.Purpose, NewPassword: i.Body.NewPassword})
		}),
	}
}

// profileRoutes 当前用户的资料读取与修改。
func profileRoutes() []httpapi.Route[*Service] {
	return []httpapi.Route[*Service]{
		httpapi.ItemEndpoint(operation("Profile", "get-me", "GET", "/v1/me", "获取当前用户与登录账号", httpapi.Session), func(s *Service, c context.Context, _ *struct{}) (Profile, error) { return s.Me(c, request(c)) }, profileDTO),
		httpapi.ItemEndpoint(operation("Profile", "update-me", "PATCH", "/v1/me", "修改展示资料", httpapi.Session), func(s *Service, c context.Context, i *UpdateInput) (UserRecord, error) {
			return s.UpdateProfile(c, request(c), i.Body.DisplayName)
		}, func(v UserRecord) User { return User(v) }),
	}
}

// securityRoutes 重新认证、密码与联系邮箱变更。
func securityRoutes() []httpapi.Route[*Service] {
	return []httpapi.Route[*Service]{
		httpapi.MapEndpoint(operation("Account Security", "reauthenticate-user", "POST", "/v1/me/reauthenticate", "为敏感操作重新认证", httpapi.Session), func(s *Service, c context.Context, i *ReauthenticateInput) (AuthenticationResult, error) {
			return s.Reauthenticate(c, request(c), Reauthentication{Method: i.Body.Method, Password: i.Body.Password, AccountID: i.Body.AccountID, Operation: i.Body.Operation, Target: i.Body.Target})
		}, reauthenticateOutput),
		httpapi.NoContentEndpoint(operation("Account Security", "set-password", "PUT", "/v1/me/password", "新增或修改密码", httpapi.Session), func(s *Service, c context.Context, i *PasswordInput) error {
			id := uuid.Nil
			if i.Body.ReauthenticationID != nil {
				id = *i.Body.ReauthenticationID
			}
			return s.SetPassword(c, request(c), i.Body.CurrentPassword, i.Body.NewPassword, id)
		}),
		httpapi.NoContentEndpoint(func() huma.Operation {
			op := operation("Account Security", "change-email", "POST", "/v1/me/email", "发送联系邮箱变更验证", httpapi.Session)
			op.DefaultStatus = 202
			return op
		}(), func(s *Service, c context.Context, i *EmailInput) error {
			return s.ChangeEmail(c, request(c), i.Body.Email, i.Body.ReauthenticationID)
		}),
	}
}

// linkedAccountRoutes 登录账号的显式绑定、确认与解绑。
func linkedAccountRoutes() []httpapi.Route[*Service] {
	return []httpapi.Route[*Service]{
		httpapi.MapEndpoint(operation("Linked Accounts", "link-account", "POST", "/v1/me/accounts/link", "开始绑定第三方账号", httpapi.Session), func(s *Service, c context.Context, i *LinkInput) (FlowResult, error) {
			return s.StartLink(c, request(c), i.Body.Provider, i.Body.ReauthenticationID)
		}, func(v FlowResult) *FlowOutput { return &FlowOutput{CacheControl: "no-store", Body: flowDTO(v)} }),
		httpapi.NoContentEndpoint(operation("Linked Accounts", "confirm-account-link", "POST", "/v1/me/accounts/link/confirm", "确认绑定账号", httpapi.Session), func(s *Service, c context.Context, i *ConfirmLinkInput) error {
			return s.ConfirmLink(c, request(c), i.Body.FlowID)
		}),
		httpapi.NoContentEndpoint(operation("Linked Accounts", "unlink-account", "POST", "/v1/me/accounts/{id}/unlink", "解除登录账号绑定", httpapi.Session), func(s *Service, c context.Context, i *UnlinkInput) error {
			return s.Unlink(c, request(c), i.ID, i.Body.ReauthenticationID)
		}),
	}
}

// sessionRoutes 当前用户的会话查询与撤销。
func sessionRoutes() []httpapi.Route[*Service] {
	return []httpapi.Route[*Service]{
		httpapi.PageEndpoint[SessionListResponse](operation("Sessions", "list-sessions", "GET", "/v1/me/sessions", "列出自己的会话", httpapi.Session), func(s *Service, c context.Context, i *ListInput) (pagination.Result[SessionRecord], error) {
			return s.Sessions(c, request(c), i.Params())
		}, func(v SessionRecord) Session { return Session(v) }),
		httpapi.NoContentEndpoint(operation("Sessions", "revoke-session", "DELETE", "/v1/me/sessions/{id}", "撤销指定会话或退出当前设备", httpapi.Session), func(s *Service, c context.Context, i *IDInput) error {
			return s.RevokeSession(c, request(c), i.ID, false)
		}),
		httpapi.NoContentEndpoint(operation("Sessions", "revoke-my-sessions", "DELETE", "/v1/me/sessions", "退出全部设备", httpapi.Session), func(s *Service, c context.Context, _ *struct{}) error {
			return s.RevokeSession(c, request(c), uuid.Nil, true)
		}),
	}
}

func callbackOutput(v AuthenticationResult) *CallbackOutput {
	body := AuthCallbackResponse{Result: v.Result}
	switch v.Result {
	case ResultSession:
		session := sessionDTO(v.Session)
		body.Session = &session
	case ResultReauthenticated:
		body.ReauthenticationID = &v.ReauthenticationID
	case ResultLinkPending:
		body.FlowID = &v.Flow.ID
		body.Provider = &v.Provider
		body.Name = &v.Name
	}
	return &CallbackOutput{CacheControl: "no-store", Body: body}
}
func reauthenticateOutput(v AuthenticationResult) *ReauthenticateOutput {
	body := UserReauthenticateResponse{Result: v.Result}
	if v.Result == ResultRedirect {
		f := flowDTO(v.Flow)
		body.Flow = &f
	} else {
		body.ReauthenticationID = &v.ReauthenticationID
	}
	return &ReauthenticateOutput{CacheControl: "no-store", Body: body}
}
