//go:build integration

package integration_test

import (
	"encoding/json"
	app "github.com/example/go-starter-kit/internal/app/api"
	"github.com/example/go-starter-kit/internal/modules/account"
	"github.com/example/go-starter-kit/internal/modules/project"
	"github.com/example/go-starter-kit/internal/platform/password"
	"github.com/example/go-starter-kit/tests/integration/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func applicationServices(t *testing.T, pool *pgxpool.Pool) app.Dependencies {
	t.Helper()
	deps, err := app.NewServices(app.Config{AuthSessionIdleTTL: 30 * time.Minute, AuthSessionMaxTTL: 24 * time.Hour, FrontendURL: "https://web.example"}, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return deps
}
func TestSessionProtectsBusinessResources(t *testing.T) {
	pool, _ := testutil.Database(t)
	deps := applicationServices(t, pool)
	h, e := password.New(1)
	if e != nil {
		t.Fatal(e)
	}
	hash, e := h.Hash(t.Context(), "integration password phrase")
	if e != nil {
		t.Fatal(e)
	}
	for _, email := range []string{"owner@example.com", "other@example.com"} {
		_, e = pool.Exec(t.Context(), `WITH u AS (INSERT INTO users(id,email,email_normalized,email_verified_at,status) VALUES($3,$1,$1,now(),'active') RETURNING id) INSERT INTO accounts(id,user_id,provider_id,provider_namespace,provider_account_id,password_hash) SELECT $4,id,'credential','local',id::text,$2 FROM u`, email, hash, uuid.New(), uuid.New())
		if e != nil {
			t.Fatal(e)
		}
	}
	handler, _, e := app.NewHandler(app.Config{RequestTimeout: time.Second}, slog.New(slog.NewTextHandler(io.Discard, nil)), deps)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	call := func(method, path, token, body string, want int) []byte {
		t.Helper()
		req, _ := http.NewRequestWithContext(t.Context(), method, server.URL+path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		res, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("%s %s status=%d", method, path, res.StatusCode)
		}
		return data
	}
	var owner, other account.SessionCreateResponse
	if e = json.Unmarshal(call(http.MethodPost, "/v1/auth/login", "", `{"email":"owner@example.com","password":"integration password phrase"}`, http.StatusOK), &owner); e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(call(http.MethodPost, "/v1/auth/login", "", `{"email":"other@example.com","password":"integration password phrase"}`, http.StatusOK), &other); e != nil {
		t.Fatal(e)
	}
	var createdProject project.Project
	if e = json.Unmarshal(call(http.MethodPost, "/v1/projects", owner.Token, `{"name":"private"}`, http.StatusCreated), &createdProject); e != nil {
		t.Fatal(e)
	}
	call(http.MethodGet, "/v1/projects/"+createdProject.ID.String(), other.Token, "", http.StatusNotFound)
	call(http.MethodDelete, "/v1/me/sessions/"+owner.SessionID.String(), owner.Token, "", http.StatusNoContent)
	call(http.MethodGet, "/v1/projects/"+createdProject.ID.String(), owner.Token, "", http.StatusUnauthorized)
}
