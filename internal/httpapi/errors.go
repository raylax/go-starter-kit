package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/example/go-starter-kit/internal/apperror"
)

// sanitizeErrors 避免校验器把密码、令牌或原始请求正文带入错误响应。
// 字段路径和解析消息也可能包含用户输入，因此仅保留通用校验提示。
func sanitizeErrors(_ huma.Context, _ string, value any) (any, error) {
	if model, ok := value.(*huma.ErrorModel); ok && len(model.Errors) > 0 {
		copy := *model
		copy.Errors = []*huma.ErrorDetail{{Message: "request validation failed"}}
		return &copy, nil
	}
	return value, nil
}

func Problem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(huma.ErrorModel{Status: status, Title: http.StatusText(status), Detail: detail})
}

// InternalError 统一处理业务模块未映射的异常。
func InternalError(ctx context.Context, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		logFailure(ctx, "handle_request", "operation failed", context.DeadlineExceeded)
		return huma.Error504GatewayTimeout("request deadline exceeded")
	}
	logFailure(ctx, "handle_request", "operation failed", err)
	return huma.Error500InternalServerError("internal server error")
}

// logFailure 统一记录固定阶段和安全分类，禁止把错误原文写入请求日志。
func logFailure(ctx context.Context, stage, message string, err error) {
	attrs := append([]slog.Attr{slog.String("stage", stage)}, apperror.DiagnosticAttrs(err)...)
	Logger(ctx).LogAttrs(ctx, slog.LevelError, message, attrs...)
}

// FromError 只公开已分类业务错误；未知错误记录安全分类并返回通用提示。
func FromError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	var business *apperror.Error
	if errors.As(err, &business) {
		switch business.Kind() {
		case apperror.NotFound:
			return huma.Error404NotFound(business.Error())
		case apperror.Conflict:
			return huma.Error409Conflict(business.Error())
		case apperror.Invalid:
			return huma.Error422UnprocessableEntity(business.Error())
		case apperror.Unauthenticated:
			return huma.Error401Unauthorized(business.Error())
		case apperror.Forbidden:
			return huma.Error403Forbidden(business.Error())
		case apperror.RateLimited:
			response := huma.Error429TooManyRequests(business.Error())
			if delay, ok := apperror.RetryAfter(err); ok {
				return huma.ErrorWithHeaders(response, http.Header{"Retry-After": []string{strconv.FormatInt(int64(math.Ceil(delay.Seconds())), 10)}})
			}
			return response
		case apperror.Unavailable:
			logFailure(ctx, "handle_request", "dependency unavailable", err)
			return huma.Error503ServiceUnavailable(business.Error())
		}
		return InternalError(ctx, err)
	}
	// HTTP 层显式声明的错误保持原语义，例如健康检查超时仍为 503。
	var statusError huma.StatusError
	if errors.As(err, &statusError) {
		return statusError
	}
	return InternalError(ctx, err)
}
