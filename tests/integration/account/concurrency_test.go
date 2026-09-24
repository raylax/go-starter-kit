//go:build integration

package account_test

import (
	"context"
	"errors"
	"github.com/example/go-starter-kit/internal/modules/account"
	"net/url"
	"testing"
	"time"

	"github.com/example/go-starter-kit/internal/apperror"
	"github.com/example/go-starter-kit/internal/authorization"
	"github.com/example/go-starter-kit/internal/pagination"
)

func fixtureSubject(t *testing.T, f *accountFixture, token string) account.Request {
	t.Helper()
	user, session, err := f.s.AuthenticateSession(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	return account.Request{Subject: authorization.Subject{UserID: user, SessionID: session}, ClientIP: "concurrency-test"}
}

func TestSessionsDoNotWaitForUserLock(t *testing.T) {
	f := newFixture(t)
	session := f.register("sessions-lock@example.com")
	r := fixtureSubject(t, f, session.Token)
	tx, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(t.Context(), "SELECT id FROM users WHERE id=$1 FOR UPDATE", r.UserID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	page, err := f.s.Sessions(ctx, r, pagination.Params{Limit: 20})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("只读列表不应等待用户锁: count=%d err=%v", len(page.Items), err)
	}
}

func TestProofIssuanceDoesNotSerializeWithUserUpdates(t *testing.T) {
	f := newFixture(t)
	session := f.register("proof-lock@example.com")
	r := fixtureSubject(t, f, session.Token)
	tx, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	// 普通字段更新的锁允许外键检查；证明签发不应额外请求排他用户锁。
	if _, err = tx.Exec(t.Context(), "UPDATE users SET display_name='pending' WHERE id=$1", r.UserID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	proof, err := f.s.Reauthenticate(ctx, r, account.Reauthentication{Method: account.MethodPassword, Password: testPassword, Operation: account.OperationLinkAccount, Target: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	flow, err := f.s.StartLink(ctx, r, "demo", proof.ReauthenticationID)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(flow.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.s.Callback(ctx, account.Request{ClientIP: r.ClientIP}, flow.Token, "proof-lock-social", u.Query().Get("state"))
	if err != nil || result.Result != account.ResultLinkPending {
		t.Fatalf("证明回调不应等待用户锁: %v", err)
	}
	emailProof, err := f.s.Reauthenticate(ctx, r, account.Reauthentication{Method: account.MethodPassword, Password: testPassword, Operation: account.OperationChangeEmail, Target: "proof-new@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.ChangeEmail(ctx, r, "proof-new@example.com", emailProof.ReauthenticationID); err != nil {
		t.Fatalf("邮箱挑战签发不应等待用户锁: %v", err)
	}
	if err = tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = f.s.RevokeSession(t.Context(), r, session.SessionID, false); err != nil {
		t.Fatal(err)
	}
	if err = f.s.ConfirmLink(t.Context(), r, flow.ID); !errors.Is(err, apperror.ErrUnauthenticated) {
		t.Fatalf("已撤销原会话的证明不得完成绑定: %v", err)
	}
	token := f.challenge("proof-new@example.com", "change_email")
	if err = f.s.VerifyChallenge(t.Context(), account.Request{ClientIP: r.ClientIP}, token, account.Verification{Purpose: account.VerifyChangeEmail}); !errors.Is(err, account.ErrVerification) {
		t.Fatalf("已撤销原会话的证明不得修改邮箱: %v", err)
	}
}

func TestConcurrentLinkProofIsClaimedOnce(t *testing.T) {
	f := newFixture(t)
	session := f.register("link-race@example.com")
	r := fixtureSubject(t, f, session.Token)
	proof := f.reauth(session.Token, testPassword, "link_account", "demo")
	type outcome struct {
		flow account.FlowResult
		err  error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			<-start
			flow, err := f.s.StartLink(t.Context(), r, "demo", proof)
			results <- outcome{flow, err}
		}()
	}
	close(start)
	var winner account.FlowResult
	success := 0
	for range 2 {
		result := <-results
		if result.err == nil {
			success++
			winner = result.flow
		} else if !errors.Is(result.err, account.ErrReauthentication) {
			t.Fatalf("认领竞争应返回证明失效: %v", result.err)
		}
	}
	var flows int
	if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM auth_flows WHERE reauthentication_id=$1", proof).Scan(&flows); err != nil {
		t.Fatal(err)
	}
	if success != 1 || flows != 1 {
		t.Fatalf("一次性认领或失败回滚错误: success=%d flows=%d", success, flows)
	}
	u, err := url.Parse(winner.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	callbacks := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := f.s.Callback(t.Context(), account.Request{ClientIP: r.ClientIP}, winner.Token, "link-race-social", u.Query().Get("state"))
			callbacks <- err
		}()
	}
	success = 0
	for range 2 {
		if err := <-callbacks; err == nil {
			success++
		} else if !errors.Is(err, account.ErrFlow) {
			t.Fatalf("重复回调应返回流程失效: %v", err)
		}
	}
	if success != 1 {
		t.Fatalf("流程回调成功 %d 次", success)
	}
	confirmations := make(chan error, 2)
	for range 2 {
		go func() { confirmations <- f.s.ConfirmLink(t.Context(), r, winner.ID) }()
	}
	for range 2 {
		if err := <-confirmations; err != nil {
			t.Fatalf("重复确认应保持幂等: %v", err)
		}
	}
	var accounts, audits int
	if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM accounts WHERE user_id=$1 AND provider_id='demo'", r.UserID).Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM audit_events WHERE scope_subject=$1 AND action='account.link' AND outcome='success'", r.UserID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if accounts != 1 || audits != 1 {
		t.Fatalf("重复绑定产生副作用: accounts=%d audits=%d", accounts, audits)
	}
}

func TestPasswordRecoveryKeepsSnapshotVersion(t *testing.T) {
	f := newFixture(t)
	session := f.register("recovery-race@example.com")
	r := fixtureSubject(t, f, session.Token)
	tx, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(t.Context(), "UPDATE users SET email='recovery-changed@example.com', email_normalized='recovery-changed@example.com', auth_version=auth_version+1 WHERE id=$1", r.UserID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- f.s.ForgotPassword(ctx, account.Request{ClientIP: r.ClientIP}, "recovery-race@example.com")
	}()
	for {
		var waiting bool
		err = f.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE '-- name: CreateVerification%')").Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	var staleVersion bool
	if err = f.pool.QueryRow(t.Context(), "SELECT v.auth_version<u.auth_version FROM auth_verifications v JOIN users u ON u.id=v.user_id WHERE v.user_id=$1 AND v.purpose='reset_password'", r.UserID).Scan(&staleVersion); err != nil || !staleVersion {
		t.Fatalf("旧邮箱挑战不得使用新版本: stale=%v err=%v", staleVersion, err)
	}
	token := f.challenge("recovery-race@example.com", "reset_password")
	err = f.s.VerifyChallenge(t.Context(), account.Request{ClientIP: r.ClientIP}, token, account.Verification{Purpose: account.VerifyResetPassword, NewPassword: "a password from an obsolete proof"})
	if !errors.Is(err, account.ErrVerification) {
		t.Fatalf("竞争产生的过期证明必须被拒绝: %v", err)
	}
	f.login("recovery-changed@example.com", testPassword)
}
