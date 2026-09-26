package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/example/go-starter-kit/internal/identity"
)

type tokenAuthenticator struct {
	calls int
	token string
}

type authenticationFailure struct{ err error }

func (a authenticationFailure) Authenticate(context.Context, string) (identity.Principal, error) {
	return identity.Principal{}, a.err
}

func TestAuthenticationFailureDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, reason string
		err          error
		status       int
	}{
		{name: "依赖故障", reason: "internal_error", err: errors.New("private-authentication-error"), status: http.StatusServiceUnavailable},
		{name: "请求超时", reason: "deadline_exceeded", err: context.DeadlineExceeded, status: http.StatusGatewayTimeout},
		{name: "无效凭据", err: identity.ErrUnauthorized, status: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			handler, api := New(Config{RequestTimeout: time.Second}, slog.New(slog.NewJSONHandler(&logs, nil)))
			huma.Register(api, huma.Operation{
				OperationID: "diagnostic-auth", Method: http.MethodGet, Path: "/protected",
				Middlewares: huma.Middlewares{Middleware(api, authenticationFailure{test.err})},
			}, func(context.Context, *struct{}) (*struct{}, error) {
				t.Fatal("认证失败后仍然执行了处理函数")
				return nil, nil
			})
			request := httptest.NewRequest(http.MethodGet, "/protected", nil)
			request.Header.Set("Authorization", "Bearer private-session-token")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("错误响应状态：%d", response.Code)
			}
			found := false
			decoder := json.NewDecoder(&logs)
			for decoder.More() {
				var entry map[string]any
				if err := decoder.Decode(&entry); err != nil {
					t.Fatal(err)
				}
				serialized, err := json.Marshal(entry)
				if err != nil || strings.Contains(string(serialized), "private-") {
					t.Fatal("认证日志泄露错误原文或会话令牌")
				}
				if entry["msg"] == "authentication failed" {
					found = true
					if entry["stage"] != "authenticate_session" || entry["reason_code"] != test.reason || entry["request_id"] != response.Header().Get("X-Request-ID") {
						t.Fatal("认证诊断缺少分类或请求关联")
					}
				}
			}
			if found != (test.reason != "") {
				t.Fatal("依赖失败应记录诊断，无效凭据不应记录基础设施错误")
			}
		})
	}
}

func (a *tokenAuthenticator) Authenticate(_ context.Context, token string) (identity.Principal, error) {
	a.calls++
	a.token = token
	return identity.Principal{Subject: "alice", SessionID: "session-test"}, nil
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
				if result.Code != http.StatusNoContent || a.calls != 1 || a.token != "token" || result.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("合法请求解析错误：status=%d calls=%d", result.Code, a.calls)
				}
			} else if result.Code != http.StatusUnauthorized || a.calls != 0 || result.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatalf("非法请求头没有被 HTTP 层拒绝：status=%d calls=%d", result.Code, a.calls)
			}
		})
	}
}
