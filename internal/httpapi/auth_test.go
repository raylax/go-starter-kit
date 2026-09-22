package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/example/go-starter-kit/internal/identity"
)

type tokenAuthenticator struct {
	calls int
	token string
}

func (a *tokenAuthenticator) Authenticate(_ context.Context, token string) (string, error) {
	a.calls++
	a.token = token
	return "alice", nil
}

func TestBearerTransport(t *testing.T) {
	for _, header := range []string{"", "Bearer", "Basic token", "Bearer token extra", "bearer token", "Bearer token"} {
		t.Run(header, func(t *testing.T) {
			a := &tokenAuthenticator{}
			handler, api := New(Config{RequestTimeout: time.Second}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			huma.Register(api, huma.Operation{OperationID: "protected", Method: http.MethodGet, Path: "/protected", Middlewares: huma.Middlewares{Middleware(api, a)}}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
				if identity.Subject(ctx) != "alice" {
					t.Error("认证用户没有传入处理函数")
				}
				return nil, nil
			})
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("Authorization", header)
			result := httptest.NewRecorder()
			handler.ServeHTTP(result, req)
			valid := header == "bearer token" || header == "Bearer token"
			if valid {
				if result.Code != 204 || a.calls != 1 || a.token != "token" || result.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("合法请求解析错误：status=%d calls=%d", result.Code, a.calls)
				}
			} else if result.Code != 401 || a.calls != 0 || result.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatalf("非法请求头没有被 HTTP 层拒绝：status=%d calls=%d", result.Code, a.calls)
			}
		})
	}
}
