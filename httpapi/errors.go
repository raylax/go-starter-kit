package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/example/go-starter-kit/apperror"
)

func Problem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(huma.ErrorModel{Status: status, Title: http.StatusText(status), Detail: detail})
}

// InternalError 统一处理业务模块未映射的异常。
func InternalError(ctx context.Context, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return huma.Error504GatewayTimeout("request deadline exceeded")
	}
	Logger(ctx).ErrorContext(ctx, "operation failed", "error", err)
	return huma.Error500InternalServerError("internal server error")
}

// FromError 只公开已分类业务错误；未知错误记录原因并返回通用提示。
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
			return huma.Error401Unauthorized("valid bearer token required")
		case apperror.Unavailable:
			Logger(ctx).ErrorContext(ctx, "dependency unavailable", "error", err)
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
