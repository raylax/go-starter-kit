package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/example/go-starter-kit/internal/apperror"
)

type sensitiveRequest struct {
	Password string `json:"password" maxLength:"4"`
}

type sensitiveInput struct {
	Body sensitiveRequest
}

func TestValidationDoesNotEchoCredentials(t *testing.T) {
	handler, api := New(Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	huma.Register(api, huma.Operation{OperationID: "sensitive", Method: http.MethodPost, Path: "/sensitive"}, func(context.Context, *sensitiveInput) (*struct{}, error) {
		return &struct{}{}, nil
	})
	for _, body := range []string{
		`{"password":"private-credential"}`,
		`{"password":"private-credential",`,
		`{"private-credential":true}`,
	} {
		r := httptest.NewRequest(http.MethodPost, "/sensitive", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code < http.StatusBadRequest || strings.Contains(w.Body.String(), "private-credential") || strings.Contains(w.Body.String(), `"value"`) {
			t.Fatal("校验响应泄露请求内容或未拒绝无效请求")
		}
	}
}

func TestErrorMapping(t *testing.T) {
	secret := errors.New("private SQL host and constraint details")
	for _, tc := range []struct {
		name   string
		err    error
		status int
		detail string
	}{
		{"不存在", apperror.Wrap(apperror.New(apperror.NotFound, "project not found"), secret), http.StatusNotFound, "project not found"},
		{"冲突", apperror.Wrap(apperror.New(apperror.Conflict, "name exists"), secret), http.StatusConflict, "name exists"},
		{"输入无效", apperror.New(apperror.Invalid, "invalid project input"), http.StatusUnprocessableEntity, "invalid project input"},
		{"未认证", apperror.ErrUnauthenticated, http.StatusUnauthorized, "authenticated subject required"},
		{"依赖故障", apperror.Wrap(apperror.New(apperror.Unavailable, "service unavailable"), secret), http.StatusServiceUnavailable, "service unavailable"},
		{"超时", fmt.Errorf("query: %w", context.DeadlineExceeded), http.StatusGatewayTimeout, "request deadline exceeded"},
		{"未知错误", secret, http.StatusInternalServerError, "internal server error"},
		{"未知分类", apperror.New(255, secret.Error()), http.StatusInternalServerError, "internal server error"},
		{"HTTP 错误", fmt.Errorf("wrapped: %w", huma.Error503ServiceUnavailable("service not ready")), http.StatusServiceUnavailable, "service not ready"},
		{"底层 HTTP 错误不能覆盖业务提示", apperror.Wrap(apperror.New(apperror.Invalid, "invalid input"), huma.Error500InternalServerError(secret.Error())), http.StatusUnprocessableEntity, "invalid input"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			ctx := context.WithValue(t.Context(), loggerKey{}, slog.New(slog.NewJSONHandler(&logs, nil)))
			err := FromError(ctx, tc.err)
			var model *huma.ErrorModel
			if !errors.As(err, &model) || model.Status != tc.status || model.Detail != tc.detail || strings.Contains(model.Detail, secret.Error()) {
				t.Fatalf("状态或对外提示不正确：%v", err)
			}
			if strings.Contains(logs.String(), secret.Error()) {
				t.Fatal("日志泄露内部错误原文")
			}
		})
	}
	if FromError(t.Context(), nil) != nil {
		t.Fatal("成功结果不应映射为错误")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := FromError(ctx, context.Canceled)
	if err.(huma.StatusError).GetStatus() == http.StatusUnauthorized {
		t.Fatal("请求取消不能归类为无效凭据")
	}
	deadlineCtx, stop := context.WithTimeout(t.Context(), 0)
	defer stop()
	if FromError(deadlineCtx, secret).(huma.StatusError).GetStatus() != http.StatusGatewayTimeout {
		t.Fatal("底层错误丢失超时信息时应检查上下文")
	}
	if FromError(deadlineCtx, huma.Error503ServiceUnavailable("service not ready")).(huma.StatusError).GetStatus() != http.StatusServiceUnavailable {
		t.Fatal("健康检查的显式状态被改写")
	}
}

func TestOperationFailureDiagnostics(t *testing.T) {
	const secret = "private-password-token-and-host"
	for _, test := range []struct {
		name, reason, sqlState string
		err                    error
		status                 int
		expiredContext         bool
	}{
		{name: "未知故障", reason: "internal_error", err: errors.New(secret), status: http.StatusInternalServerError},
		{name: "数据库故障", reason: "database_error", sqlState: "08006", err: apperror.Wrap(apperror.New(apperror.Unavailable, "service unavailable"), &pgconn.PgError{Code: "08006", Message: secret, Detail: secret}), status: http.StatusServiceUnavailable},
		{name: "依赖不可用", reason: "dependency_unavailable", err: apperror.Wrap(apperror.New(apperror.Unavailable, "service unavailable"), errors.New(secret)), status: http.StatusServiceUnavailable},
		{name: "底层超时", reason: "deadline_exceeded", err: fmt.Errorf("%s: %w", secret, context.DeadlineExceeded), status: http.StatusGatewayTimeout},
		{name: "上下文超时", reason: "deadline_exceeded", err: errors.New(secret), status: http.StatusGatewayTimeout, expiredContext: true},
		{name: "请求取消", reason: "canceled", err: fmt.Errorf("%s: %w", secret, context.Canceled), status: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil)).With("request_id", "test-request")
			ctx := context.WithValue(t.Context(), loggerKey{}, logger)
			if test.expiredContext {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 0)
				defer cancel()
			}
			if err := FromError(ctx, test.err); err.(huma.StatusError).GetStatus() != test.status {
				t.Fatalf("诊断改变了错误状态: %v", err)
			}
			if strings.Contains(logs.String(), secret) {
				t.Fatal("诊断泄露错误原文")
			}
			var entry map[string]any
			if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
				t.Fatalf("每次故障应恰好记录一条诊断: %v", err)
			}
			if entry["stage"] != "handle_request" || entry["reason_code"] != test.reason || entry["request_id"] != "test-request" {
				t.Fatalf("诊断缺少固定阶段、分类或请求关联: %v", entry)
			}
			if code, _ := entry["sqlstate"].(string); code != test.sqlState {
				t.Fatalf("SQLSTATE 未正确保留: %q", code)
			}
			if _, ok := entry["error"]; ok {
				t.Fatal("诊断不能记录原始错误对象")
			}
		})
	}
}

func TestEndpointMapsRawBusinessError(t *testing.T) {
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("test", "1"))
	route := Endpoint(NewOperation(Public, huma.Operation{OperationID: "missing", Method: http.MethodGet, Path: "/missing"}), func(_ struct{}, _ context.Context, _ *struct{}) (*struct{}, error) {
		return nil, apperror.Wrap(apperror.New(apperror.NotFound, "project not found"), errors.New("private SQL detail"))
	})
	route.Bind(api, struct{}{})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if recorder.Code != http.StatusNotFound || strings.Contains(recorder.Body.String(), "private SQL") || !strings.Contains(recorder.Body.String(), "project not found") {
		t.Fatalf("路由未统一映射业务错误：%s", recorder.Body.String())
	}
}

func TestRetryAfterComesFromBusinessError(t *testing.T) {
	for _, delay := range []time.Duration{time.Second, 1500 * time.Millisecond, time.Minute, 15 * time.Minute} {
		route := Endpoint(NewOperation(Public, huma.Operation{OperationID: "limited", Method: http.MethodGet, Path: "/limited"}), func(struct{}, context.Context, *struct{}) (*struct{}, error) {
			return nil, apperror.WithRetryAfter(apperror.New(apperror.RateLimited, "limited"), delay)
		})
		router := chi.NewRouter()
		api := humachi.New(router, huma.DefaultConfig("test", "1"))
		route.Bind(api, struct{}{})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/limited", nil))
		seconds, err := strconv.Atoi(recorder.Header().Get("Retry-After"))
		if recorder.Code != http.StatusTooManyRequests || err != nil || seconds != int(math.Ceil(delay.Seconds())) {
			t.Fatalf("等待时间映射错误: %s", recorder.Header().Get("Retry-After"))
		}
	}
}
