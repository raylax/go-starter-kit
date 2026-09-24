//go:build integration

package account_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/example/go-starter-kit/internal/modules/account"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/example/go-starter-kit/internal/httpapi"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/example/go-starter-kit/internal/platform/password"
	"github.com/example/go-starter-kit/tests/integration/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testPassword = "a secure test password phrase"

type sessionAdapter struct{ s *account.Service }

func (a sessionAdapter) Authenticate(ctx context.Context, raw string) (identity.Principal, error) {
	u, s, e := a.s.AuthenticateSession(ctx, raw)
	return identity.Principal{Subject: u, SessionID: s}, e
}

type accountFixture struct {
	t       *testing.T
	s       *account.Service
	pool    *pgxpool.Pool
	server  *httptest.Server
	options account.Options
	deps    account.Dependencies
}

func newFixture(t *testing.T, configure ...func(*account.Options)) *accountFixture {
	t.Helper()
	pool, _ := testutil.Database(t)
	h, e := password.New(4)
	if e != nil {
		t.Fatal(e)
	}
	f := &accountFixture{t: t, pool: pool, options: account.Options{IdleTTL: 30 * time.Minute, MaxTTL: 24 * time.Hour, FrontendURL: "https://web.example"}}
	for _, apply := range configure {
		apply(&f.options)
	}
	f.deps = account.Dependencies{Authorizer: account.NewAdminChecker(pool), Passwords: h, Federation: testFederation{t}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	f.s = f.newService(pool, f.deps)
	handler, api := httpapi.New(httpapi.Config{RequestTimeout: 10 * time.Second}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, route := range append(account.Routes(), account.AdminRoutes()...) {
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
func (f *accountFixture) newService(database account.Database, deps account.Dependencies) *account.Service {
	f.t.Helper()
	service, err := account.NewService(database, f.options, deps)
	if err != nil {
		f.t.Fatal(err)
	}
	return service
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
	if want == http.StatusTooManyRequests && res.Header.Get("Retry-After") == "" {
		f.t.Fatal("缺少重试期限")
	}
	return out
}
func safeError(raw []byte) string {
	var err huma.ErrorModel
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
		var body string
		if err := rows.Scan(&body); err != nil {
			f.t.Fatal(err)
		}
		_, after, found := strings.Cut(body, `href="`)
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
func (f *accountFixture) register(email string) account.SessionCreateResponse {
	f.t.Helper()
	f.call(http.MethodPost, "/v1/auth/register", "", map[string]any{"email": email}, http.StatusAccepted)
	token := f.challenge(email, "register")
	f.call(http.MethodPost, "/v1/auth/verify", "", map[string]any{"token": token, "purpose": "register", "new_password": testPassword}, http.StatusNoContent)
	return f.login(email, testPassword)
}
func (f *accountFixture) login(email, pw string) account.SessionCreateResponse {
	return decode[account.SessionCreateResponse](f.t, f.call(http.MethodPost, "/v1/auth/login", "", map[string]any{"email": email, "password": pw}, http.StatusOK))
}
func (f *accountFixture) reauth(token, pw, operation, target string) uuid.UUID {
	v := decode[account.UserReauthenticateResponse](f.t, f.call(http.MethodPost, "/v1/me/reauthenticate", token, map[string]any{"method": "password", "password": pw, "operation": operation, "target": target}, http.StatusOK))
	if v.ReauthenticationID == nil {
		f.t.Fatal("缺少重新认证引用")
	}
	return *v.ReauthenticationID
}
func (f *accountFixture) callback(flow account.AuthFlowResponse, code string, want int) account.AuthCallbackResponse {
	u, e := url.Parse(flow.AuthorizationURL)
	if e != nil {
		f.t.Fatal(e)
	}
	data := f.call(http.MethodPost, "/v1/auth/oauth/callback", "", map[string]any{"token": flow.Token, "code": code, "state": u.Query().Get("state")}, want)
	if want != http.StatusOK {
		return account.AuthCallbackResponse{}
	}
	return decode[account.AuthCallbackResponse](f.t, data)
}
