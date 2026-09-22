package testutil

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/example/go-starter-kit/httpapi"
	"github.com/example/go-starter-kit/identity"
)

// ModuleServer 只装配待测模块，避免模块测试依赖整个应用。
func ModuleServer[S any](t *testing.T, subject string, timeout time.Duration, service S, routes []httpapi.Route[S]) *httptest.Server {
	t.Helper()
	authenticator, err := identity.NewDevelopment("integration-only-token", subject)
	if err != nil {
		t.Fatal(err)
	}
	handler, api := httpapi.New(httpapi.Config{RequestTimeout: timeout}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, route := range routes {
		route.Bind(api, service, httpapi.Middleware(api, authenticator))
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func Request(t *testing.T) func(*httptest.Server, string, string, string, bool, int) []byte {
	ctx := t.Context()
	return func(server *httptest.Server, method, path, body string, authorized bool, want int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if authorized {
			req.Header.Set("Authorization", "Bearer integration-only-token")
		}
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		requestID, err := uuid.Parse(response.Header.Get("X-Request-ID"))
		if err != nil || requestID.Version() != 7 {
			t.Fatal("响应缺少有效的 UUID v7 请求 ID")
		}
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, response.StatusCode, want, data)
		}
		if want >= 400 && !strings.HasPrefix(response.Header.Get("Content-Type"), "application/problem+json") {
			t.Fatalf("invalid error content type: %s", response.Header.Get("Content-Type"))
		}
		return data
	}
}
