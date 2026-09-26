//go:build integration

package account_test

import (
	"net/http"
	"testing"

	"github.com/example/go-starter-kit/internal/modules/account"
)

func TestStringIdentifiersAcrossAuthenticationAndAuthorization(t *testing.T) {
	f := newFixture(t)
	const userID, accountID, sessionID, proofID = "user_alice", "account_password", "session_current", "proof_link"
	hash, err := f.deps.Passwords.Hash(t.Context(), testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(t.Context(), "INSERT INTO users(id,email,email_normalized,email_verified_at,status,role,recovery_enabled) VALUES($1,'strings@example.com','strings@example.com',now(),'active','admin',true)", userID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(t.Context(), "INSERT INTO accounts(id,user_id,provider_id,provider_namespace,provider_account_id,password_hash) VALUES($1,$2,'credential','local',$2,$3)", accountID, userID, hash); err != nil {
		t.Fatal(err)
	}
	session := f.login("strings@example.com", testPassword)
	if _, err := f.pool.Exec(t.Context(), "UPDATE user_sessions SET id=$1 WHERE id=$2", sessionID, session.SessionID); err != nil {
		t.Fatal(err)
	}
	profile := decode[account.UserProfile](t, f.call(http.MethodGet, "/v1/me", session.Token, nil, http.StatusOK))
	if profile.User.ID != userID || profile.SessionID != sessionID || len(profile.Accounts) != 1 || profile.Accounts[0].ID != accountID {
		t.Fatal("认证或资料映射改变了字符串标识")
	}
	f.call(http.MethodGet, "/v1/admin/users", session.Token, nil, http.StatusOK)
	proof := f.reauth(session.Token, testPassword, "link_account", "demo")
	if _, err := f.pool.Exec(t.Context(), "UPDATE auth_flows SET id=$1 WHERE id=$2", proofID, proof); err != nil {
		t.Fatal(err)
	}
	flow := decode[account.AuthFlowResponse](t, f.call(http.MethodPost, "/v1/me/accounts/link", session.Token, map[string]any{"provider": "demo", "reauthentication_id": proofID}, http.StatusOK))
	f.callback(flow, "strings-social", http.StatusOK)
	f.call(http.MethodPost, "/v1/me/accounts/link/confirm", session.Token, map[string]any{"flow_id": flow.FlowID}, http.StatusNoContent)
	// 使用第三方账号证明控制权，再解绑具有非 UUID 标识的密码账号。
	profile = decode[account.UserProfile](t, f.call(http.MethodGet, "/v1/me", session.Token, nil, http.StatusOK))
	var socialID string
	for _, linked := range profile.Accounts {
		if linked.Provider == "demo" {
			socialID = linked.ID
		}
	}
	if socialID == "" {
		t.Fatal("绑定未持久化")
	}
	result := decode[account.UserReauthenticateResponse](t, f.call(http.MethodPost, "/v1/me/reauthenticate", session.Token, map[string]any{"method": "oauth", "account_id": socialID, "operation": "unlink_account", "target": accountID}, http.StatusOK))
	if result.Flow == nil {
		t.Fatal("重新认证缺少流程")
	}
	verified := f.callback(*result.Flow, "strings-social", http.StatusOK)
	f.call(http.MethodPost, "/v1/me/accounts/"+accountID+"/unlink", session.Token, map[string]any{"reauthentication_id": verified.ReauthenticationID}, http.StatusNoContent)
	f.call(http.MethodGet, "/v1/me", session.Token, nil, http.StatusUnauthorized)
}
