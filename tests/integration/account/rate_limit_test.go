//go:build integration

package account_test

import (
	"errors"
	"github.com/example/go-starter-kit/internal/modules/account"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/example/go-starter-kit/internal/apperror"
)

func TestOAuthUsesOnlySourceBudgetAndReportsRemainingWindow(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 60; i++ {
		f.call(http.MethodPost, "/v1/auth/oauth/demo", "", map[string]any{"purpose": "login"}, http.StatusOK)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.server.URL+"/v1/auth/oauth/demo", strings.NewReader(`{"purpose":"login"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	seconds, err := strconv.Atoi(res.Header.Get("Retry-After"))
	if res.StatusCode != http.StatusTooManyRequests || err != nil || seconds < 1 || seconds > 60 {
		t.Fatalf("来源限流状态或等待时间错误: status=%d retry=%d", res.StatusCode, seconds)
	}
	if _, err = f.pool.Exec(t.Context(), "UPDATE auth_rate_limits SET expires_at=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	f.call(http.MethodPost, "/v1/auth/oauth/demo", "", map[string]any{"purpose": "login"}, http.StatusOK)
	// 已知主体仍保持独立的十五分钟保护，不随 OAuth 来源策略一起放宽。
	for i := 0; i < 10; i++ {
		if _, err := f.s.Login(t.Context(), account.Request{ClientIP: "subject-test"}, "person@example.com", "wrong"); !errors.Is(err, account.ErrCredentials) {
			t.Fatal(err)
		}
	}
	_, err = f.s.Login(t.Context(), account.Request{ClientIP: "subject-test"}, "person@example.com", "wrong")
	delay, ok := apperror.RetryAfter(err)
	if !errors.Is(err, account.ErrRateLimited) || !ok || delay <= time.Minute || delay > 15*time.Minute {
		t.Fatalf("主体限流窗口错误: delay=%v", delay)
	}
}
