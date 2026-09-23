package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/example/go-starter-kit/internal/identity"
)

// Middleware 解析 Bearer 请求头，完成认证并向业务层传递用户标识。
func Middleware(api huma.API, authenticator identity.Authenticator) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		raw, ok := bearer(ctx.Header("Authorization"))
		var principal identity.Principal
		err := identity.ErrUnauthorized
		if ok {
			principal, err = authenticator.Authenticate(ctx.Context(), raw)
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Context().Err(), context.DeadlineExceeded) {
			_ = huma.WriteErr(api, ctx, http.StatusGatewayTimeout, "authentication deadline exceeded")
			return
		}
		if err != nil && !errors.Is(err, identity.ErrUnauthorized) {
			_ = huma.WriteErr(api, ctx, http.StatusServiceUnavailable, "authentication service unavailable")
			return
		}
		if err != nil || principal.Subject == "" {
			ctx.SetHeader("WWW-Authenticate", "Bearer")
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "session_invalid")
			return
		}
		ctx.SetHeader("Cache-Control", "no-store")
		next(huma.WithContext(ctx, identity.WithPrincipal(ctx.Context(), principal)))
	}
}

func bearer(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	return parts[1], true
}
