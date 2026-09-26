//go:build integration

package account_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/example/go-starter-kit/internal/modules/account"
	"github.com/google/uuid"
)

func TestVerifiedEmailReclaimsPendingRegistration(t *testing.T) {
	for _, tc := range []struct {
		name    string
		expired bool
	}{
		{name: "原注册证明仍有效"},
		{name: "原注册证明已过期", expired: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			const email = "claimed@example.com"
			f.call(http.MethodPost, "/v1/auth/register", "", map[string]any{"email": email}, http.StatusAccepted)
			registrationToken := f.challenge(email, "register")
			var pendingID uuid.UUID
			if err := f.pool.QueryRow(t.Context(), "SELECT id FROM users WHERE email_normalized=$1", email).Scan(&pendingID); err != nil {
				t.Fatal(err)
			}
			if tc.expired {
				if _, err := f.pool.Exec(t.Context(), "UPDATE auth_verifications SET expires_at=now()-interval '1 second' WHERE user_id=$1 AND purpose='register'", pendingID); err != nil {
					t.Fatal(err)
				}
			}

			flow := decode[account.AuthFlowResponse](t, f.call(http.MethodPost, "/v1/auth/oauth/demo", "", map[string]any{"purpose": "register"}, http.StatusOK))
			signed := f.callback(flow, "existing-social-user", http.StatusOK)
			if signed.Session == nil {
				t.Fatal("第三方注册没有会话")
			}
			session := *signed.Session
			before := decode[account.UserProfile](t, f.call(http.MethodGet, "/v1/me", session.Token, nil, http.StatusOK))
			if before.User.Email != nil || len(before.Accounts) != 1 {
				t.Fatal("第三方用户应只有一个登录方式且尚未关联邮箱")
			}
			proof := decode[account.UserReauthenticateResponse](t, f.call(http.MethodPost, "/v1/me/reauthenticate", session.Token, map[string]any{
				"method": "oauth", "account_id": before.Accounts[0].ID, "operation": "change_email", "target": email,
			}, http.StatusOK))
			if proof.Flow == nil {
				t.Fatal("第三方重新认证没有创建流程")
			}
			reauthenticated := f.callback(*proof.Flow, "existing-social-user", http.StatusOK)
			if reauthenticated.ReauthenticationID == nil {
				t.Fatal("第三方重新认证没有返回证明引用")
			}
			f.call(http.MethodPost, "/v1/me/email", session.Token, map[string]any{"email": email, "reauthentication_id": reauthenticated.ReauthenticationID}, http.StatusAccepted)
			f.call(http.MethodPost, "/v1/auth/verify", "", map[string]any{"token": f.challenge(email, "change_email"), "purpose": "change_email"}, http.StatusNoContent)
			f.call(http.MethodGet, "/v1/me", session.Token, nil, http.StatusUnauthorized)

			// 旧注册证明不得在新归属建立后激活占位用户或创建密码账号。
			body := f.call(http.MethodPost, "/v1/auth/verify", "", map[string]any{"token": registrationToken, "purpose": "register", "new_password": testPassword}, http.StatusUnprocessableEntity)
			if safeError(body) != "verification_invalid" {
				t.Fatal("旧注册证明没有返回验证失效")
			}
			f.call(http.MethodPost, "/v1/auth/login", "", map[string]any{"email": email, "password": testPassword}, http.StatusUnauthorized)
			flow = decode[account.AuthFlowResponse](t, f.call(http.MethodPost, "/v1/auth/oauth/demo", "", map[string]any{"purpose": "login"}, http.StatusOK))
			signed = f.callback(flow, "existing-social-user", http.StatusOK)
			if signed.Session == nil {
				t.Fatal("邮箱认领后第三方登录没有会话")
			}
			after := decode[account.UserProfile](t, f.call(http.MethodGet, "/v1/me", signed.Session.Token, nil, http.StatusOK))
			if after.User.ID != before.User.ID || after.User.Email == nil || *after.User.Email != email || !after.User.EmailVerified || len(after.Accounts) != 1 || after.Accounts[0].ID != before.Accounts[0].ID {
				t.Fatal("邮箱认领改变了原主体或登录方式")
			}
			var pendingPreserved bool
			if err := f.pool.QueryRow(t.Context(), "SELECT status='pending' AND email IS NULL AND email_normalized IS NULL AND NOT EXISTS(SELECT 1 FROM accounts WHERE user_id=users.id) FROM users WHERE id=$1", pendingID).Scan(&pendingPreserved); err != nil || !pendingPreserved {
				t.Fatalf("占位用户应保留且解除邮箱归属: preserved=%v err=%v", pendingPreserved, err)
			}
		})
	}
}

func TestVerifiedEmailCannotBeReclaimed(t *testing.T) {
	f := newFixture(t)
	owner := f.register("owned@example.com")
	claimant := f.register("claimant@example.com")
	ownerBefore := decode[account.UserProfile](t, f.call(http.MethodGet, "/v1/me", owner.Token, nil, http.StatusOK))
	claimantBefore := decode[account.UserProfile](t, f.call(http.MethodGet, "/v1/me", claimant.Token, nil, http.StatusOK))
	proof := f.reauth(claimant.Token, testPassword, "change_email", "owned@example.com")
	f.call(http.MethodPost, "/v1/me/email", claimant.Token, map[string]any{"email": "owned@example.com", "reauthentication_id": proof}, http.StatusAccepted)
	f.call(http.MethodPost, "/v1/auth/verify", "", map[string]any{"token": f.challenge("owned@example.com", "change_email"), "purpose": "change_email"}, http.StatusConflict)
	for _, expected := range []struct {
		session account.SessionCreateResponse
		profile account.UserProfile
	}{
		{owner, ownerBefore},
		{claimant, claimantBefore},
	} {
		profile := decode[account.UserProfile](t, f.call(http.MethodGet, "/v1/me", expected.session.Token, nil, http.StatusOK))
		if profile.User.ID != expected.profile.User.ID || profile.User.Email == nil || *profile.User.Email != *expected.profile.User.Email || !profile.User.EmailVerified {
			t.Fatal("邮箱冲突修改了已有归属或会话")
		}
	}
}

func TestEmailClaimRacesRegistrationVerification(t *testing.T) {
	f := newFixture(t)
	const targetEmail = "registration-race@example.com"
	f.call(http.MethodPost, "/v1/auth/register", "", map[string]any{"email": targetEmail}, http.StatusAccepted)
	registrationToken := f.challenge(targetEmail, "register")
	claimant := f.register("claim-race@example.com")
	claimantBefore := decode[account.UserProfile](t, f.call(http.MethodGet, "/v1/me", claimant.Token, nil, http.StatusOK))
	proof := f.reauth(claimant.Token, testPassword, "change_email", targetEmail)
	f.call(http.MethodPost, "/v1/me/email", claimant.Token, map[string]any{"email": targetEmail, "reauthentication_id": proof}, http.StatusAccepted)
	changeToken := f.challenge(targetEmail, "change_email")

	// 持有占位用户锁，让注册验证和邮箱认领都进入等待后再同时放行。
	tx, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(t.Context(), "SELECT id FROM users WHERE email_normalized=$1 FOR UPDATE", targetEmail); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	type outcome struct {
		purpose account.VerificationPurpose
		err     error
	}
	results := make(chan outcome, 2)
	go func() {
		err := f.s.VerifyChallenge(ctx, account.Request{ClientIP: "register-race"}, registrationToken, account.Verification{Purpose: account.VerifyRegister, NewPassword: testPassword})
		results <- outcome{account.VerifyRegister, err}
	}()
	go func() {
		err := f.s.VerifyChallenge(ctx, account.Request{ClientIP: "email-claim-race"}, changeToken, account.Verification{Purpose: account.VerifyChangeEmail})
		results <- outcome{account.VerifyChangeEmail, err}
	}()
	for {
		var waiting int
		if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'").Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	successes := 0
	var winner account.VerificationPurpose
	for range 2 {
		result := <-results
		if result.err == nil {
			successes++
			winner = result.purpose
		} else if !errors.Is(result.err, account.ErrConflict) && !errors.Is(result.err, account.ErrVerification) {
			t.Fatalf("邮箱归属竞争没有安全失败: %v", result.err)
		}
	}
	if successes != 1 {
		t.Fatalf("同一邮箱应只有一个验证成功，实际为 %d", successes)
	}
	owner := f.login(targetEmail, testPassword)
	ownerProfile := decode[account.UserProfile](t, f.call(http.MethodGet, "/v1/me", owner.Token, nil, http.StatusOK))
	if !ownerProfile.User.EmailVerified {
		t.Fatal("竞争获胜者没有已验证邮箱")
	}
	if winner == account.VerifyChangeEmail {
		if ownerProfile.User.ID != claimantBefore.User.ID {
			t.Fatal("邮箱认领创建或合并了用户")
		}
		f.call(http.MethodGet, "/v1/me", claimant.Token, nil, http.StatusUnauthorized)
	} else {
		if ownerProfile.User.ID == claimantBefore.User.ID {
			t.Fatal("注册成功后邮箱被其他用户夺走")
		}
		profile := decode[account.UserProfile](t, f.call(http.MethodGet, "/v1/me", claimant.Token, nil, http.StatusOK))
		if profile.User.Email == nil || *profile.User.Email != *claimantBefore.User.Email {
			t.Fatal("失败的邮箱认领改变了原邮箱")
		}
	}
}
