//go:build integration

package account_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/example/go-starter-kit/internal/authorization"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/example/go-starter-kit/internal/modules/account"
	"github.com/google/uuid"
)

func TestAccountHTTP(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	f.call(http.MethodPost, "/v1/auth/register", "", map[string]any{"email": "alice@example.com", "role": "admin"}, http.StatusUnprocessableEntity)
	alice := f.register("alice@example.com")
	bob := f.register("bob@example.com")
	if !strings.HasPrefix(alice.Token, "tk_") || strings.Contains(alice.Token, ".") {
		t.Fatal("会话令牌格式错误")
	}
	me := decode[account.UserProfile](t, f.call(http.MethodGet, "/v1/me", alice.Token, nil, http.StatusOK))
	if me.User.Role != "user" || !me.User.EmailVerified || len(me.Accounts) != 1 {
		t.Fatal("注册模型错误")
	}
	f.call(http.MethodPatch, "/v1/me", alice.Token, map[string]any{"display_name": "Alice", "role": "admin"}, http.StatusUnprocessableEntity)
	f.call(http.MethodPatch, "/v1/me", alice.Token, map[string]any{"display_name": "Alice"}, http.StatusOK)
	f.call(http.MethodGet, "/v1/admin/users", alice.Token, nil, http.StatusForbidden)
	f.call(http.MethodDelete, "/v1/me/sessions/"+alice.SessionID.String(), bob.Token, nil, http.StatusNotFound)
	f.call(http.MethodGet, "/v1/me", alice.SessionID.String(), nil, http.StatusUnauthorized)
	f.call(http.MethodGet, "/v1/me", f.challenge("alice@example.com", "register"), nil, http.StatusUnauthorized)
	f.call(http.MethodPost, "/v1/auth/verify", "", map[string]any{"token": f.challenge("alice@example.com", "register"), "purpose": "register", "new_password": testPassword}, http.StatusUnprocessableEntity)
	f.call(http.MethodPost, "/v1/auth/login", "", map[string]any{"email": "alice@example.com", "password": "wrong"}, http.StatusUnauthorized)
	f.call(http.MethodPost, "/v1/me/reauthenticate", alice.Token, map[string]any{"method": "password", "password": testPassword, "operation": "unlink_account", "target": me.Accounts[0].ID.String()}, http.StatusConflict)

	t.Run("第三方绑定与目的隔离", func(t *testing.T) {
		proof := f.reauth(alice.Token, testPassword, "link_account", "demo")
		flow := decode[account.AuthFlowResponse](t, f.call(http.MethodPost, "/v1/me/accounts/link", alice.Token, map[string]any{"provider": "demo", "reauthentication_id": proof}, http.StatusOK))
		f.call(http.MethodPost, "/v1/me/accounts/link", alice.Token, map[string]any{"provider": "demo", "reauthentication_id": proof}, http.StatusUnprocessableEntity)
		u, _ := url.Parse(flow.AuthorizationURL)
		f.call(http.MethodPost, "/v1/auth/oauth/callback", "", map[string]any{"token": flow.Token, "code": "alice-social", "state": "wrong"}, http.StatusUnprocessableEntity)
		pending := f.callback(flow, "alice-social", http.StatusOK)
		if pending.Result != "link_pending" {
			t.Fatal("绑定回调不应直接创建会话")
		}
		f.call(http.MethodPost, "/v1/auth/oauth/callback", "", map[string]any{"token": flow.Token, "code": "alice-social", "state": u.Query().Get("state")}, http.StatusUnprocessableEntity)
		f.call(http.MethodPost, "/v1/me/accounts/link/confirm", bob.Token, map[string]any{"flow_id": flow.FlowID}, http.StatusUnprocessableEntity)
		f.call(http.MethodPost, "/v1/me/accounts/link/confirm", alice.Token, map[string]any{"flow_id": flow.FlowID}, http.StatusNoContent)
		f.call(http.MethodPost, "/v1/me/accounts/link/confirm", alice.Token, map[string]any{"flow_id": flow.FlowID}, http.StatusNoContent)
		loginFlow := decode[account.AuthFlowResponse](t, f.call(http.MethodPost, "/v1/auth/oauth/demo", "", map[string]any{"purpose": "login"}, http.StatusOK))
		signed := f.callback(loginFlow, "alice-social", http.StatusOK)
		if signed.Session == nil {
			t.Fatal("第三方登录没有会话")
		}
		socialMe := decode[account.UserProfile](t, f.call(http.MethodGet, "/v1/me", signed.Session.Token, nil, http.StatusOK))
		if socialMe.User.ID != me.User.ID {
			t.Fatal("同一用户的资源主体发生变化")
		}
		unknown := decode[account.AuthFlowResponse](t, f.call(http.MethodPost, "/v1/auth/oauth/demo", "", map[string]any{"purpose": "login"}, http.StatusOK))
		f.callback(unknown, "new-subject", http.StatusConflict)
		register := decode[account.AuthFlowResponse](t, f.call(http.MethodPost, "/v1/auth/oauth/demo", "", map[string]any{"purpose": "register"}, http.StatusOK))
		fresh := f.callback(register, "new-subject", http.StatusOK)
		external := decode[account.UserProfile](t, f.call(http.MethodGet, "/v1/me", fresh.Session.Token, nil, http.StatusOK))
		if external.User.Email != nil || len(external.Accounts) != 1 || external.Accounts[0].Provider == "credential" {
			t.Fatal("第三方用户创建了占位密码或邮箱")
		}
		bobProof := f.reauth(bob.Token, testPassword, "link_account", "demo")
		conflict := decode[account.AuthFlowResponse](t, f.call(http.MethodPost, "/v1/me/accounts/link", bob.Token, map[string]any{"provider": "demo", "reauthentication_id": bobProof}, http.StatusOK))
		f.callback(conflict, "alice-social", http.StatusOK)
		f.call(http.MethodPost, "/v1/me/accounts/link/confirm", bob.Token, map[string]any{"flow_id": conflict.FlowID}, http.StatusConflict)
	})

	t.Run("改密原子性与即时撤销", func(t *testing.T) {
		if _, e := f.pool.Exec(ctx, "ALTER TABLE audit_events RENAME TO test_unavailable_audit_events"); e != nil {
			t.Fatal(e)
		}
		f.call(http.MethodPut, "/v1/me/password", alice.Token, map[string]any{"current_password": testPassword, "new_password": "a different secure phrase"}, http.StatusInternalServerError)
		f.call(http.MethodGet, "/v1/me", alice.Token, nil, http.StatusOK)
		if _, e := f.pool.Exec(ctx, "ALTER TABLE test_unavailable_audit_events RENAME TO audit_events"); e != nil {
			t.Fatal(e)
		}
		f.call(http.MethodPut, "/v1/me/password", alice.Token, map[string]any{"current_password": testPassword, "new_password": "a different secure phrase"}, http.StatusNoContent)
		f.call(http.MethodGet, "/v1/me", alice.Token, nil, http.StatusUnauthorized)
		alice = f.login("alice@example.com", "a different secure phrase")
	})

	t.Run("密码恢复与验证用途", func(t *testing.T) {
		f.call(http.MethodPost, "/v1/auth/password/forgot", "", map[string]any{"email": "missing@example.com"}, http.StatusAccepted)
		f.call(http.MethodPost, "/v1/auth/password/forgot", "", map[string]any{"email": "alice@example.com"}, http.StatusAccepted)
		token := f.challenge("alice@example.com", "reset_password")
		f.call(http.MethodPost, "/v1/auth/verify", "", map[string]any{"token": token, "purpose": "change_email", "new_password": testPassword}, http.StatusUnprocessableEntity)
		f.call(http.MethodPost, "/v1/auth/verify", "", map[string]any{"token": token, "purpose": "register", "new_password": testPassword}, http.StatusUnprocessableEntity)
		f.call(http.MethodPost, "/v1/auth/verify", "", map[string]any{"token": token, "purpose": "reset_password", "new_password": testPassword}, http.StatusNoContent)
		f.call(http.MethodGet, "/v1/me", alice.Token, nil, http.StatusUnauthorized)
		alice = f.login("alice@example.com", testPassword)
	})
	t.Run("邮箱变更与重放", func(t *testing.T) {
		proof := f.reauth(alice.Token, testPassword, "change_email", "newalice@example.com")
		f.call(http.MethodPost, "/v1/me/email", alice.Token, map[string]any{"email": "newalice@example.com", "reauthentication_id": proof}, http.StatusAccepted)
		token := f.challenge("newalice@example.com", "change_email")
		f.call(http.MethodPost, "/v1/auth/verify", "", map[string]any{"token": token, "purpose": "change_email"}, http.StatusNoContent)
		f.call(http.MethodGet, "/v1/me", alice.Token, nil, http.StatusUnauthorized)
		f.call(http.MethodPost, "/v1/auth/verify", "", map[string]any{"token": token, "purpose": "change_email"}, http.StatusUnprocessableEntity)
		alice = f.login("newalice@example.com", testPassword)
	})
	t.Run("管理员与会话撤销", func(t *testing.T) {
		bobMe := decode[account.UserProfile](t, f.call(http.MethodGet, "/v1/me", bob.Token, nil, http.StatusOK))
		if _, e := f.pool.Exec(ctx, "UPDATE users SET role='admin', auth_version=auth_version+1 WHERE id=$1", me.User.ID); e != nil {
			t.Fatal(e)
		}
		f.call(http.MethodGet, "/v1/me", alice.Token, nil, http.StatusUnauthorized)
		alice = f.login("newalice@example.com", testPassword)
		f.call(http.MethodGet, "/v1/admin/users", alice.Token, nil, http.StatusOK)
		f.call(http.MethodPatch, "/v1/admin/users/"+bobMe.User.ID.String()+"/status", alice.Token, map[string]any{"status": "disabled"}, http.StatusOK)
		f.call(http.MethodGet, "/v1/me", bob.Token, nil, http.StatusUnauthorized)
		f.call(http.MethodPatch, "/v1/admin/users/"+bobMe.User.ID.String()+"/status", alice.Token, map[string]any{"status": "active"}, http.StatusOK)
		f.call(http.MethodGet, "/v1/me", bob.Token, nil, http.StatusUnauthorized)
		bob = f.login("bob@example.com", testPassword)
		f.call(http.MethodDelete, "/v1/admin/users/"+bobMe.User.ID.String()+"/sessions", alice.Token, nil, http.StatusNoContent)
		f.call(http.MethodGet, "/v1/me", bob.Token, nil, http.StatusUnauthorized)
		f.call(http.MethodDelete, "/v1/me/sessions/"+alice.SessionID.String(), alice.Token, nil, http.StatusNoContent)
		f.call(http.MethodGet, "/v1/me", alice.Token, nil, http.StatusUnauthorized)
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
	if e := f.s.Register(ctx, account.Request{ClientIP: "test"}, email); e != nil {
		t.Fatal(e)
	}
	token := f.challenge(email, "register")
	results := make(chan error, 2)
	for range 2 {
		go func() {
			results <- f.s.VerifyChallenge(ctx, account.Request{ClientIP: "test"}, token, account.Verification{Purpose: "register", NewPassword: testPassword})
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
	f.call(http.MethodGet, "/v1/me", session.Token, nil, http.StatusOK)
	var remaining float64
	if e := f.pool.QueryRow(ctx, "SELECT extract(epoch from idle_expires_at-now())::float8 FROM user_sessions WHERE id=$1", session.SessionID).Scan(&remaining); e != nil || remaining < 1700 {
		t.Fatalf("会话没有续期: %v", e)
	}
	if _, e := f.pool.Exec(ctx, "UPDATE user_sessions SET idle_expires_at=now()-interval '1 second' WHERE id=$1", session.SessionID); e != nil {
		t.Fatal(e)
	}
	f.call(http.MethodGet, "/v1/me", session.Token, nil, http.StatusUnauthorized)
	for i := 0; i < 11; i++ {
		want := http.StatusUnauthorized
		if i == 10 {
			want = http.StatusTooManyRequests
		}
		f.call(http.MethodPost, "/v1/auth/login", "", map[string]any{"email": "limited@example.com", "password": "wrong"}, want)
	}
}

func TestFederatedUserPasswordAndUnlink(t *testing.T) {
	f := newFixture(t)
	login := func(purpose string) account.SessionCreateResponse {
		flow := decode[account.AuthFlowResponse](t, f.call(http.MethodPost, "/v1/auth/oauth/demo", "", map[string]any{"purpose": purpose}, http.StatusOK))
		return *f.callback(flow, "social-only", http.StatusOK).Session
	}
	session := login("register")
	me := decode[account.UserProfile](t, f.call(http.MethodGet, "/v1/me", session.Token, nil, http.StatusOK))
	social := me.Accounts[0].ID
	proof := func(operation, target string) uuid.UUID {
		result := decode[account.UserReauthenticateResponse](t, f.call(http.MethodPost, "/v1/me/reauthenticate", session.Token, map[string]any{
			"method": "oauth", "account_id": social, "operation": operation, "target": target,
		}, http.StatusOK))
		return *f.callback(*result.Flow, "social-only", http.StatusOK).ReauthenticationID
	}
	beforeEmail := proof("set_password", "credential")
	f.call(http.MethodPut, "/v1/me/password", session.Token, map[string]any{"new_password": testPassword, "reauthentication_id": beforeEmail}, http.StatusUnprocessableEntity)
	emailProof := proof("change_email", "social@example.com")
	f.call(http.MethodPost, "/v1/me/email", session.Token, map[string]any{"email": "social@example.com", "reauthentication_id": emailProof}, http.StatusAccepted)
	f.call(http.MethodPost, "/v1/auth/verify", "", map[string]any{"token": f.challenge("social@example.com", "change_email"), "purpose": "change_email"}, http.StatusNoContent)
	f.call(http.MethodGet, "/v1/me", session.Token, nil, http.StatusUnauthorized)
	session = login("login")
	f.call(http.MethodPost, "/v1/auth/password/forgot", "", map[string]any{"email": "social@example.com"}, http.StatusAccepted)
	var resets int
	if e := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM auth_verifications WHERE user_id=$1 AND purpose='reset_password'", me.User.ID).Scan(&resets); e != nil || resets != 0 {
		t.Fatal("恢复流程不能为仅第三方用户创建本地凭据")
	}
	passwordProof := proof("set_password", "credential")
	f.call(http.MethodPut, "/v1/me/password", session.Token, map[string]any{"new_password": testPassword, "reauthentication_id": passwordProof}, http.StatusNoContent)
	f.call(http.MethodGet, "/v1/me", session.Token, nil, http.StatusUnauthorized)
	session = f.login("social@example.com", testPassword)
	unlinkProof := f.reauth(session.Token, testPassword, "unlink_account", social.String())
	f.call(http.MethodPost, "/v1/me/accounts/"+social.String()+"/unlink", session.Token, map[string]any{"reauthentication_id": unlinkProof}, http.StatusNoContent)
	f.call(http.MethodGet, "/v1/me", session.Token, nil, http.StatusUnauthorized)
	session = f.login("social@example.com", testPassword)
	me = decode[account.UserProfile](t, f.call(http.MethodGet, "/v1/me", session.Token, nil, http.StatusOK))
	if len(me.Accounts) != 1 || me.Accounts[0].Provider != "credential" {
		t.Fatal("解绑后登录方式错误")
	}
	f.call(http.MethodPost, "/v1/me/reauthenticate", session.Token, map[string]any{"method": "password", "password": testPassword, "operation": "unlink_account", "target": me.Accounts[0].ID}, http.StatusConflict)
}

func TestLoginCannotRacePasswordChange(t *testing.T) {
	f := newFixture(t)
	session := f.register("concurrent@example.com")
	me := decode[account.UserProfile](t, f.call(http.MethodGet, "/v1/me", session.Token, nil, http.StatusOK))
	original := f.deps.Passwords
	deps := f.deps
	verified, resume := make(chan struct{}), make(chan struct{})
	deps.Passwords = pausedPasswords{Passwords: original, verify: func(ctx context.Context, hash, password string) (bool, bool, error) {
		ok, upgrade, err := original.Verify(ctx, hash, password)
		close(verified)
		select {
		case <-resume:
		case <-ctx.Done():
			return false, false, ctx.Err()
		}
		return ok, upgrade, err
	}}
	login := f.newService(f.pool, deps)
	result := make(chan error, 1)
	go func() {
		_, err := login.Login(t.Context(), account.Request{ClientIP: "concurrent"}, "concurrent@example.com", testPassword)
		result <- err
	}()
	select {
	case <-verified:
	case <-time.After(10 * time.Second):
		t.Fatal("密码验证未完成")
	}
	err := f.s.SetPassword(t.Context(), account.Request{Subject: authorization.Subject{UserID: me.User.ID.String(), SessionID: session.SessionID.String()}}, testPassword, "a freshly changed password", uuid.Nil)
	close(resume)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, account.ErrCredentials) {
		t.Fatalf("并发登录未拒绝旧凭据: %v", err)
	}
	f.call(http.MethodGet, "/v1/me", session.Token, nil, http.StatusUnauthorized)
}

func TestProofTokensUseBodyAndDoNotInvalidateSession(t *testing.T) {
	f := newFixture(t)
	session := f.register("session@example.com")
	f.call(http.MethodPost, "/v1/auth/register", "", map[string]any{"email": "verify@example.com"}, http.StatusAccepted)
	token := f.challenge("verify@example.com", "register")
	// Authorization 不能作为验证令牌的备用来源。
	f.call(http.MethodPost, "/v1/auth/verify", token, map[string]any{"purpose": "register", "new_password": testPassword}, http.StatusUnprocessableEntity)
	body := f.call(http.MethodPost, "/v1/auth/verify", session.Token, map[string]any{"token": session.Token, "purpose": "register", "new_password": testPassword}, http.StatusUnprocessableEntity)
	if safeError(body) != "verification_invalid" || bytes.Contains(body, []byte(session.Token)) {
		t.Fatal("验证错误缺少稳定提示或泄露令牌")
	}
	f.call(http.MethodPost, "/v1/auth/verify", session.Token, map[string]any{"token": token, "purpose": "register", "new_password": testPassword}, http.StatusNoContent)
	flow := decode[account.AuthFlowResponse](t, f.call(http.MethodPost, "/v1/auth/oauth/demo", "", map[string]any{"purpose": "register"}, http.StatusOK))
	u, _ := url.Parse(flow.AuthorizationURL)
	f.call(http.MethodPost, "/v1/auth/oauth/callback", flow.Token, map[string]any{"code": "body-subject", "state": u.Query().Get("state")}, http.StatusUnprocessableEntity)
	body = f.call(http.MethodPost, "/v1/auth/oauth/callback", session.Token, map[string]any{"token": token, "code": "body-subject", "state": u.Query().Get("state")}, http.StatusUnprocessableEntity)
	if safeError(body) != "flow_invalid" {
		t.Fatal("流程错误没有独立语义")
	}
	f.call(http.MethodPost, "/v1/auth/oauth/callback", session.Token, map[string]any{"token": flow.Token, "code": "body-subject", "state": u.Query().Get("state")}, http.StatusOK)
	body = f.call(http.MethodPost, "/v1/me/reauthenticate", session.Token, map[string]any{"method": "password", "password": "wrong", "operation": "link_account", "target": "demo"}, http.StatusUnprocessableEntity)
	if safeError(body) != "reauthentication_invalid" {
		t.Fatal("重新认证错误没有独立语义")
	}
	f.call(http.MethodGet, "/v1/me", session.Token, nil, http.StatusOK)
	f.call(http.MethodGet, "/v1/me", flow.Token, nil, http.StatusUnauthorized)
}

// testFederation 只替换外部第三方，账户流程仍使用真实数据库。
type testFederation struct{ t *testing.T }

func (f testFederation) Enabled(id string) bool { return id == "demo" || id == "second" }
func (f testFederation) Version(string) string  { return "v1" }
func (f testFederation) Start(_ context.Context, id, state, verifier string) (string, string, error) {
	if verifier == "" {
		f.t.Error("缺少 PKCE")
	}
	return "https://provider.example/authorize?state=" + state, "v1", nil
}
func (f testFederation) Verify(_ context.Context, id, version, code, verifier string) (account.VerifiedIdentity, error) {
	if code == "invalid" {
		return account.VerifiedIdentity{}, account.ErrCredentials
	}
	return account.VerifiedIdentity{Namespace: "https://" + id + ".example", Subject: code, Name: "external user", AuthenticatedAt: time.Now()}, nil
}

type pausedPasswords struct {
	account.Passwords
	verify func(context.Context, string, string) (bool, bool, error)
}

func (p pausedPasswords) Verify(ctx context.Context, hash, value string) (bool, bool, error) {
	return p.verify(ctx, hash, value)
}
