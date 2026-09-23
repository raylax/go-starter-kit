package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/example/go-starter-kit/internal/identity"
)

func TestHTTPInfrastructure(t *testing.T) {
	cfg := Config{RequestTimeout: time.Second}
	authenticator := failingAuthenticator{identity.ErrUnauthorized}
	handler := testHandler(t, cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)), authenticator, func(context.Context) error { return errors.New("private database detail") })
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/health/live", 200},
		{http.MethodGet, "/health/ready", 503},
		{http.MethodGet, "/v1/projects", 401},
		{http.MethodGet, "/does-not-exist", 404},
		{http.MethodPost, "/health/live", 405},
		{http.MethodGet, "/docs", 404},
		{http.MethodGet, "/openapi.json", 404},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
			if recorder.Code != tc.status {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if recorder.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing request ID")
			}
			if tc.status >= 400 && recorder.Header().Get("Content-Type") != "application/problem+json" {
				t.Fatalf("unexpected error format: %s", recorder.Header().Get("Content-Type"))
			}
		})
	}
}

func TestAuthenticationErrorResponses(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err       error
		status    int
		challenge bool
	}{
		{"invalid", identity.ErrUnauthorized, 401, true},
		{"deadline", context.DeadlineExceeded, 504, false},
		{"dependency", errors.New("dependency unavailable"), 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := testHandler(t, Config{RequestTimeout: time.Second}, slog.New(slog.NewTextHandler(io.Discard, nil)), failingAuthenticator{tc.err}, func(context.Context) error { return nil })
			for _, path := range []string{"/v1/projects", "/v1/tasks"} {
				r := httptest.NewRecorder()
				req := httptest.NewRequest("GET", path, nil)
				req.Header.Set("Authorization", "Bearer test-token")
				h.ServeHTTP(r, req)
				if r.Code != tc.status || (r.Header().Get("WWW-Authenticate") != "") != tc.challenge {
					t.Fatalf("%s: status=%d challenge=%q", path, r.Code, r.Header().Get("WWW-Authenticate"))
				}
				if r.Header().Get("Content-Type") != "application/problem+json" {
					t.Fatal("error format changed")
				}
			}
		})
	}
}

func TestMethodNotAllowedIncludesAllowedMethods(t *testing.T) {
	h := testHandler(t, Config{RequestTimeout: time.Second, DocsEnabled: true}, slog.New(slog.NewTextHandler(io.Discard, nil)), failingAuthenticator{identity.ErrUnauthorized}, func(context.Context) error { return nil })
	for _, tc := range []struct {
		path    string
		methods []string
	}{
		{"/health/live", []string{"GET"}},
		{"/v1/tasks", []string{"GET", "POST"}},
		{"/v1/projects/00000000-0000-0000-0000-000000000001", []string{"GET", "PUT", "DELETE"}},
		{"/openapi.json", []string{"GET"}},
	} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("PATCH", tc.path, nil))
		got := strings.FieldsFunc(strings.Join(r.Header().Values("Allow"), ","), func(r rune) bool { return r == ',' || r == ' ' })
		slices.Sort(got)
		slices.Sort(tc.methods)
		if r.Code != 405 || !slices.Equal(got, tc.methods) {
			t.Fatalf("%s: status=%d Allow=%v want=%v", tc.path, r.Code, got, tc.methods)
		}
		if r.Header().Get("Content-Type") != "application/problem+json" {
			t.Fatal("missing problem response")
		}
	}
}
