//go:build integration

package account

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/example/go-starter-kit/internal/authorization"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/example/go-starter-kit/internal/httpapi"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/example/go-starter-kit/internal/platform/password"
	"github.com/example/go-starter-kit/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testPassword = "a secure test password phrase"

type sessionAdapter struct{ s *Service }

func (a sessionAdapter) Authenticate(ctx context.Context, raw string) (identity.Principal, error) {
	u, s, e := a.s.AuthenticateSession(ctx, raw)
	return identity.Principal{Subject: u, SessionID: s}, e
}

type accountFixture struct {
	t      *testing.T
	s      *Service
	pool   *pgxpool.Pool
	server *httptest.Server
}

func newFixture(t *testing.T) *accountFixture {
	pool, _ := testutil.Database(t)
	h, e := password.New(4)
	if e != nil {
		t.Fatal(e)
	}
	f := &accountFixture{t: t, pool: pool}
	deps := Dependencies{Authorizer: authorization.AdminCheckFunc(NewAdminChecker(pool).CheckAdmin), Hash: h.Hash, Verify: h.Verify, ValidPassword: password.Validate,
		ProviderEnabled: func(id string) bool { return id == "demo" || id == "second" }, ProviderVersion: func(string) string { return "v1" },
		StartProvider: func(_ context.Context, id, state, verifier string) (string, string, error) {
			if verifier == "" {
				t.Error("缺少 PKCE")
			}
			return "https://provider.example/authorize?state=" + state, "v1", nil
		},
		VerifyProvider: func(_ context.Context, id, version, code, verifier string) (VerifiedIdentity, error) {
			if code == "invalid" {
				return VerifiedIdentity{}, ErrCredentials
			}
			return VerifiedIdentity{Namespace: "https://" + id + ".example", Subject: code, Name: "external user", AuthenticatedAt: time.Now()}, nil
		},
	}
	f.s, e = NewService(pool, Options{IdleTTL: 30 * time.Minute, MaxTTL: 24 * time.Hour, FrontendURL: "https://web.example"}, deps)
	if e != nil {
		t.Fatal(e)
	}
	handler, api := httpapi.New(httpapi.Config{RequestTimeout: 10 * time.Second, AllowedOrigins: []string{"https://web.example"}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, route := range append(Routes(), AdminRoutes()...) {
		switch route.Policy() {
		case httpapi.Session:
			route.Bind(api, f.s, httpapi.Middleware(api, sessionAdapter{f.s}))
		default:
			route.Bind(api, f.s)
		}
	}
	f.server = httptest.NewServer(handler)
	t.Cleanup(f.server.Close)
	return f
}
func (f *accountFixture) call(method, path, token string, body any, want int) []byte {
	f.t.Helper()
	var data []byte
	if body != nil {
		var e error
		data, e = json.Marshal(body)
		if e != nil {
			f.t.Fatal(e)
		}
	}
	req, e := http.NewRequestWithContext(f.t.Context(), method, f.server.URL+path, bytes.NewReader(data))
	if e != nil {
		f.t.Fatal(e)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, e := f.server.Client().Do(req)
	if e != nil {
		f.t.Fatal(e)
	}
	defer res.Body.Close()
	out, e := io.ReadAll(res.Body)
	if e != nil {
		f.t.Fatal(e)
	}
	if res.StatusCode != want {
		f.t.Fatalf("%s %s status=%d want=%d error=%s", method, path, res.StatusCode, want, safeError(out))
	}
	if res.Header.Get("Set-Cookie") != "" {
		f.t.Fatal("应用不应设置 Cookie")
	}
	if want == 429 && res.Header.Get("Retry-After") == "" {
		f.t.Fatal("缺少重试期限")
	}
	return out
}
func safeError(raw []byte) string {
	var err struct {
		Detail string `json:"detail"`
	}
	_ = json.Unmarshal(raw, &err)
	return err.Detail
}
func decode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var value T
	if e := json.Unmarshal(data, &value); e != nil {
		t.Fatal(e)
	}
	return value
}
func (f *accountFixture) challenge(email, purpose string) string {
	f.t.Helper()
	rows, err := f.pool.Query(f.t.Context(), "SELECT body FROM mail_outbox WHERE recipient=$1 AND kind=$2 ORDER BY created_at DESC, id DESC", email, purpose)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var m struct{ Body string }
		if err := rows.Scan(&m.Body); err != nil {
			f.t.Fatal(err)
		}
		_, after, found := strings.Cut(m.Body, `href="`)
		if !found {
			continue
		}
		href, _, found := strings.Cut(after, `"`)
		if !found {
			continue
		}
		u, e := url.Parse(html.UnescapeString(href))
		if e != nil {
			continue
		}
		values, e := url.ParseQuery(u.Fragment)
		if e == nil && values.Get("purpose") == purpose {
			return values.Get("token")
		}
	}
	f.t.Fatal("未收到验证邮件")
	return ""
}
func (f *accountFixture) register(email string) SessionCreateResponse {
	f.t.Helper()
	f.call("POST", "/v1/auth/register", "", map[string]any{"email": email}, 202)
	token := f.challenge(email, "register")
	f.call("POST", "/v1/auth/verify", "", map[string]any{"token": token, "purpose": "register", "new_password": testPassword}, 204)
	return f.login(email, testPassword)
}
func (f *accountFixture) login(email, pw string) SessionCreateResponse {
	return decode[SessionCreateResponse](f.t, f.call("POST", "/v1/auth/login", "", map[string]any{"email": email, "password": pw}, 200))
}
func (f *accountFixture) reauth(token, pw, operation, target string) uuid.UUID {
	v := decode[UserReauthenticateResponse](f.t, f.call("POST", "/v1/me/reauthenticate", token, map[string]any{"method": "password", "password": pw, "operation": operation, "target": target}, 200))
	if v.ReauthenticationID == nil {
		f.t.Fatal("缺少重新认证引用")
	}
	return *v.ReauthenticationID
}
func (f *accountFixture) callback(flow AuthFlowResponse, code string, want int) AuthCallbackResponse {
	u, e := url.Parse(flow.AuthorizationURL)
	if e != nil {
		f.t.Fatal(e)
	}
	data := f.call("POST", "/v1/auth/oauth/callback", "", map[string]any{"token": flow.Token, "code": code, "state": u.Query().Get("state")}, want)
	if want != 200 {
		return AuthCallbackResponse{}
	}
	return decode[AuthCallbackResponse](f.t, data)
}

func TestAccountHTTP(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	f.call("POST", "/v1/auth/register", "", map[string]any{"email": "alice@example.com", "role": "admin"}, 422)
	alice := f.register("alice@example.com")
	bob := f.register("bob@example.com")
	if !strings.HasPrefix(alice.Token, "tk_") || strings.Contains(alice.Token, ".") {
		t.Fatal("会话令牌格式错误")
	}
	me := decode[UserProfile](t, f.call("GET", "/v1/me", alice.Token, nil, 200))
	if me.User.Role != "user" || !me.User.EmailVerified || len(me.Accounts) != 1 {
		t.Fatal("注册模型错误")
	}
	f.call("PATCH", "/v1/me", alice.Token, map[string]any{"display_name": "Alice", "role": "admin"}, 422)
	f.call("PATCH", "/v1/me", alice.Token, map[string]any{"display_name": "Alice"}, 200)
	f.call("GET", "/v1/admin/users", alice.Token, nil, 403)
	f.call("DELETE", "/v1/me/sessions/"+alice.SessionID.String(), bob.Token, nil, 404)
	f.call("GET", "/v1/me", alice.SessionID.String(), nil, 401)
	f.call("GET", "/v1/me", f.challenge("alice@example.com", "register"), nil, 401)
	f.call("POST", "/v1/auth/verify", "", map[string]any{"token": f.challenge("alice@example.com", "register"), "purpose": "register", "new_password": testPassword}, 422)
	f.call("POST", "/v1/auth/login", "", map[string]any{"email": "alice@example.com", "password": "wrong"}, 401)
	f.call("POST", "/v1/me/reauthenticate", alice.Token, map[string]any{"method": "password", "password": testPassword, "operation": "unlink_account", "target": me.Accounts[0].ID.String()}, 409)

	t.Run("第三方绑定与目的隔离", func(t *testing.T) {
		proof := f.reauth(alice.Token, testPassword, "link_account", "demo")
		flow := decode[AuthFlowResponse](t, f.call("POST", "/v1/me/accounts/link", alice.Token, map[string]any{"provider": "demo", "reauthentication_id": proof}, 200))
		f.call("POST", "/v1/me/accounts/link", alice.Token, map[string]any{"provider": "demo", "reauthentication_id": proof}, 422)
		u, _ := url.Parse(flow.AuthorizationURL)
		f.call("POST", "/v1/auth/oauth/callback", "", map[string]any{"token": flow.Token, "code": "alice-social", "state": "wrong"}, 422)
		pending := f.callback(flow, "alice-social", 200)
		if pending.Result != "link_pending" {
			t.Fatal("绑定回调不应直接创建会话")
		}
		f.call("POST", "/v1/auth/oauth/callback", "", map[string]any{"token": flow.Token, "code": "alice-social", "state": u.Query().Get("state")}, 422)
		f.call("POST", "/v1/me/accounts/link/confirm", bob.Token, map[string]any{"flow_id": flow.FlowID}, 422)
		f.call("POST", "/v1/me/accounts/link/confirm", alice.Token, map[string]any{"flow_id": flow.FlowID}, 204)
		f.call("POST", "/v1/me/accounts/link/confirm", alice.Token, map[string]any{"flow_id": flow.FlowID}, 204)
		loginFlow := decode[AuthFlowResponse](t, f.call("POST", "/v1/auth/oauth/demo", "", map[string]any{"purpose": "login"}, 200))
		signed := f.callback(loginFlow, "alice-social", 200)
		if signed.Session == nil {
			t.Fatal("第三方登录没有会话")
		}
		socialMe := decode[UserProfile](t, f.call("GET", "/v1/me", signed.Session.Token, nil, 200))
		if socialMe.User.ID != me.User.ID {
			t.Fatal("同一用户的资源主体发生变化")
		}
		unknown := decode[AuthFlowResponse](t, f.call("POST", "/v1/auth/oauth/demo", "", map[string]any{"purpose": "login"}, 200))
		f.callback(unknown, "new-subject", 409)
		register := decode[AuthFlowResponse](t, f.call("POST", "/v1/auth/oauth/demo", "", map[string]any{"purpose": "register"}, 200))
		fresh := f.callback(register, "new-subject", 200)
		external := decode[UserProfile](t, f.call("GET", "/v1/me", fresh.Session.Token, nil, 200))
		if external.User.Email != nil || len(external.Accounts) != 1 || external.Accounts[0].Provider == "credential" {
			t.Fatal("第三方用户创建了占位密码或邮箱")
		}
		bobProof := f.reauth(bob.Token, testPassword, "link_account", "demo")
		conflict := decode[AuthFlowResponse](t, f.call("POST", "/v1/me/accounts/link", bob.Token, map[string]any{"provider": "demo", "reauthentication_id": bobProof}, 200))
		f.callback(conflict, "alice-social", 200)
		f.call("POST", "/v1/me/accounts/link/confirm", bob.Token, map[string]any{"flow_id": conflict.FlowID}, 409)
	})

	t.Run("改密原子性与即时撤销", func(t *testing.T) {
		if _, e := f.pool.Exec(ctx, "ALTER TABLE audit_events RENAME TO test_unavailable_audit_events"); e != nil {
			t.Fatal(e)
		}
		f.call("PUT", "/v1/me/password", alice.Token, map[string]any{"current_password": testPassword, "new_password": "a different secure phrase"}, 500)
		f.call("GET", "/v1/me", alice.Token, nil, 200)
		if _, e := f.pool.Exec(ctx, "ALTER TABLE test_unavailable_audit_events RENAME TO audit_events"); e != nil {
			t.Fatal(e)
		}
		f.call("PUT", "/v1/me/password", alice.Token, map[string]any{"current_password": testPassword, "new_password": "a different secure phrase"}, 204)
		f.call("GET", "/v1/me", alice.Token, nil, 401)
		alice = f.login("alice@example.com", "a different secure phrase")
	})

	t.Run("密码恢复与验证用途", func(t *testing.T) {
		f.call("POST", "/v1/auth/password/forgot", "", map[string]any{"email": "missing@example.com"}, 202)
		f.call("POST", "/v1/auth/password/forgot", "", map[string]any{"email": "alice@example.com"}, 202)
		token := f.challenge("alice@example.com", "reset_password")
		f.call("POST", "/v1/auth/verify", "", map[string]any{"token": token, "purpose": "change_email", "new_password": testPassword}, 422)
		f.call("POST", "/v1/auth/verify", "", map[string]any{"token": token, "purpose": "register", "new_password": testPassword}, 422)
		f.call("POST", "/v1/auth/verify", "", map[string]any{"token": token, "purpose": "reset_password", "new_password": testPassword}, 204)
		f.call("GET", "/v1/me", alice.Token, nil, 401)
		alice = f.login("alice@example.com", testPassword)
	})
	t.Run("邮箱变更与重放", func(t *testing.T) {
		proof := f.reauth(alice.Token, testPassword, "change_email", "newalice@example.com")
		f.call("POST", "/v1/me/email", alice.Token, map[string]any{"email": "newalice@example.com", "reauthentication_id": proof}, 202)
		token := f.challenge("newalice@example.com", "change_email")
		f.call("POST", "/v1/auth/verify", "", map[string]any{"token": token, "purpose": "change_email"}, 204)
		f.call("GET", "/v1/me", alice.Token, nil, 401)
		f.call("POST", "/v1/auth/verify", "", map[string]any{"token": token, "purpose": "change_email"}, 422)
		alice = f.login("newalice@example.com", testPassword)
	})
	t.Run("管理员与会话撤销", func(t *testing.T) {
		bobMe := decode[UserProfile](t, f.call("GET", "/v1/me", bob.Token, nil, 200))
		if _, e := f.pool.Exec(ctx, "UPDATE users SET role='admin', auth_version=auth_version+1 WHERE id=$1", me.User.ID); e != nil {
			t.Fatal(e)
		}
		f.call("GET", "/v1/me", alice.Token, nil, 401)
		alice = f.login("newalice@example.com", testPassword)
		f.call("GET", "/v1/admin/users", alice.Token, nil, 200)
		f.call("PATCH", "/v1/admin/users/"+bobMe.User.ID.String()+"/status", alice.Token, map[string]any{"status": "disabled"}, 200)
		f.call("GET", "/v1/me", bob.Token, nil, 401)
		f.call("PATCH", "/v1/admin/users/"+bobMe.User.ID.String()+"/status", alice.Token, map[string]any{"status": "active"}, 200)
		f.call("GET", "/v1/me", bob.Token, nil, 401)
		bob = f.login("bob@example.com", testPassword)
		f.call("DELETE", "/v1/admin/users/"+bobMe.User.ID.String()+"/sessions", alice.Token, nil, 204)
		f.call("GET", "/v1/me", bob.Token, nil, 401)
		f.call("DELETE", "/v1/me/sessions/"+alice.SessionID.String(), alice.Token, nil, 204)
		f.call("GET", "/v1/me", alice.Token, nil, 401)
	})
	var count int
	if e := f.pool.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE action='account.link' AND outcome='success'").Scan(&count); e != nil || count != 1 {
		t.Fatalf("绑定审计重复或缺失: %d %v", count, e)
	}
	if e := f.pool.QueryRow(ctx, "SELECT count(*) FROM user_sessions WHERE octet_length(token_hash)<>32").Scan(&count); e != nil || count != 0 {
		t.Fatal("会话原文持久化")
	}
	f.pool.Close()
	if _, _, e := f.s.AuthenticateSession(ctx, alice.Token); e == nil || errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("数据库故障被放行或混同无效凭据")
	}
}

func TestChallengeConcurrencyAndSessionExpiry(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	email := "race@example.com"
	if e := f.s.Register(ctx, Request{ClientIP: "test"}, email); e != nil {
		t.Fatal(e)
	}
	token := f.challenge(email, "register")
	results := make(chan error, 2)
	for range 2 {
		go func() {
			results <- f.s.VerifyChallenge(ctx, Request{ClientIP: "test"}, token, Verification{Purpose: "register", NewPassword: testPassword})
		}()
	}
	success := 0
	for range 2 {
		if e := <-results; e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("并发消费成功 %d 次", success)
	}
	session := f.login(email, testPassword)
	if _, e := f.pool.Exec(ctx, "UPDATE user_sessions SET last_seen_at=now()-interval '2 minutes', idle_expires_at=now()+interval '1 minute' WHERE id=$1", session.SessionID); e != nil {
		t.Fatal(e)
	}
	f.call("GET", "/v1/me", session.Token, nil, 200)
	var remaining float64
	if e := f.pool.QueryRow(ctx, "SELECT extract(epoch from idle_expires_at-now())::float8 FROM user_sessions WHERE id=$1", session.SessionID).Scan(&remaining); e != nil || remaining < 1700 {
		t.Fatalf("会话没有续期: %v", e)
	}
	if _, e := f.pool.Exec(ctx, "UPDATE user_sessions SET idle_expires_at=now()-interval '1 second' WHERE id=$1", session.SessionID); e != nil {
		t.Fatal(e)
	}
	f.call("GET", "/v1/me", session.Token, nil, 401)
	for i := 0; i < 11; i++ {
		want := 401
		if i == 10 {
			want = 429
		}
		f.call("POST", "/v1/auth/login", "", map[string]any{"email": "limited@example.com", "password": "wrong"}, want)
	}
}

func TestFederatedUserPasswordAndUnlink(t *testing.T) {
	f := newFixture(t)
	login := func(purpose string) SessionCreateResponse {
		flow := decode[AuthFlowResponse](t, f.call("POST", "/v1/auth/oauth/demo", "", map[string]any{"purpose": purpose}, 200))
		return *f.callback(flow, "social-only", 200).Session
	}
	session := login("register")
	me := decode[UserProfile](t, f.call("GET", "/v1/me", session.Token, nil, 200))
	social := me.Accounts[0].ID
	proof := func(operation, target string) uuid.UUID {
		result := decode[UserReauthenticateResponse](t, f.call("POST", "/v1/me/reauthenticate", session.Token, map[string]any{
			"method": "oauth", "account_id": social, "operation": operation, "target": target,
		}, 200))
		return *f.callback(*result.Flow, "social-only", 200).ReauthenticationID
	}
	beforeEmail := proof("set_password", "credential")
	f.call("PUT", "/v1/me/password", session.Token, map[string]any{"new_password": testPassword, "reauthentication_id": beforeEmail}, 422)
	emailProof := proof("change_email", "social@example.com")
	f.call("POST", "/v1/me/email", session.Token, map[string]any{"email": "social@example.com", "reauthentication_id": emailProof}, 202)
	f.call("POST", "/v1/auth/verify", "", map[string]any{"token": f.challenge("social@example.com", "change_email"), "purpose": "change_email"}, 204)
	f.call("GET", "/v1/me", session.Token, nil, 401)
	session = login("login")
	f.call("POST", "/v1/auth/password/forgot", "", map[string]any{"email": "social@example.com"}, 202)
	var resets int
	if e := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM auth_verifications WHERE user_id=$1 AND purpose='reset_password'", me.User.ID).Scan(&resets); e != nil || resets != 0 {
		t.Fatal("恢复流程不能为仅第三方用户创建本地凭据")
	}
	passwordProof := proof("set_password", "credential")
	f.call("PUT", "/v1/me/password", session.Token, map[string]any{"new_password": testPassword, "reauthentication_id": passwordProof}, 204)
	f.call("GET", "/v1/me", session.Token, nil, 401)
	session = f.login("social@example.com", testPassword)
	unlinkProof := f.reauth(session.Token, testPassword, "unlink_account", social.String())
	f.call("POST", "/v1/me/accounts/"+social.String()+"/unlink", session.Token, map[string]any{"reauthentication_id": unlinkProof}, 204)
	f.call("GET", "/v1/me", session.Token, nil, 401)
	session = f.login("social@example.com", testPassword)
	me = decode[UserProfile](t, f.call("GET", "/v1/me", session.Token, nil, 200))
	if len(me.Accounts) != 1 || me.Accounts[0].Provider != "credential" {
		t.Fatal("解绑后登录方式错误")
	}
	f.call("POST", "/v1/me/reauthenticate", session.Token, map[string]any{"method": "password", "password": testPassword, "operation": "unlink_account", "target": me.Accounts[0].ID}, 409)
}

func TestLoginCannotRacePasswordChange(t *testing.T) {
	f := newFixture(t)
	session := f.register("concurrent@example.com")
	me := decode[UserProfile](t, f.call("GET", "/v1/me", session.Token, nil, 200))
	changer := *f.s
	original := f.s.deps.Verify
	verified, resume := make(chan struct{}), make(chan struct{})
	f.s.deps.Verify = func(ctx context.Context, hash, password string) (bool, bool, error) {
		ok, upgrade, err := original(ctx, hash, password)
		close(verified)
		select {
		case <-resume:
		case <-ctx.Done():
			return false, false, ctx.Err()
		}
		return ok, upgrade, err
	}
	result := make(chan error, 1)
	go func() {
		_, err := f.s.Login(t.Context(), Request{ClientIP: "concurrent"}, "concurrent@example.com", testPassword)
		result <- err
	}()
	select {
	case <-verified:
	case <-time.After(10 * time.Second):
		t.Fatal("密码验证未完成")
	}
	err := changer.SetPassword(t.Context(), Request{Subject: authorization.Subject{UserID: me.User.ID.String(), SessionID: session.SessionID.String()}}, testPassword, "a freshly changed password", uuid.Nil)
	close(resume)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrCredentials) {
		t.Fatalf("并发登录未拒绝旧凭据: %v", err)
	}
	f.call("GET", "/v1/me", session.Token, nil, 401)
}

func TestProofTokensUseBodyAndDoNotInvalidateSession(t *testing.T) {
	f := newFixture(t)
	session := f.register("session@example.com")
	f.call("POST", "/v1/auth/register", "", map[string]any{"email": "verify@example.com"}, 202)
	token := f.challenge("verify@example.com", "register")
	// Authorization 不能作为验证令牌的备用来源。
	f.call("POST", "/v1/auth/verify", token, map[string]any{"purpose": "register", "new_password": testPassword}, 422)
	body := f.call("POST", "/v1/auth/verify", session.Token, map[string]any{"token": session.Token, "purpose": "register", "new_password": testPassword}, 422)
	if safeError(body) != "verification_invalid" || bytes.Contains(body, []byte(session.Token)) {
		t.Fatal("验证错误缺少稳定提示或泄露令牌")
	}
	f.call("POST", "/v1/auth/verify", session.Token, map[string]any{"token": token, "purpose": "register", "new_password": testPassword}, 204)
	flow := decode[AuthFlowResponse](t, f.call("POST", "/v1/auth/oauth/demo", "", map[string]any{"purpose": "register"}, 200))
	u, _ := url.Parse(flow.AuthorizationURL)
	f.call("POST", "/v1/auth/oauth/callback", flow.Token, map[string]any{"code": "body-subject", "state": u.Query().Get("state")}, 422)
	body = f.call("POST", "/v1/auth/oauth/callback", session.Token, map[string]any{"token": token, "code": "body-subject", "state": u.Query().Get("state")}, 422)
	if safeError(body) != "flow_invalid" {
		t.Fatal("流程错误没有独立语义")
	}
	f.call("POST", "/v1/auth/oauth/callback", session.Token, map[string]any{"token": flow.Token, "code": "body-subject", "state": u.Query().Get("state")}, 200)
	body = f.call("POST", "/v1/me/reauthenticate", session.Token, map[string]any{"method": "password", "password": "wrong", "operation": "link_account", "target": "demo"}, 422)
	if safeError(body) != "reauthentication_invalid" {
		t.Fatal("重新认证错误没有独立语义")
	}
	f.call("GET", "/v1/me", session.Token, nil, 200)
	f.call("GET", "/v1/me", flow.Token, nil, 401)
}
