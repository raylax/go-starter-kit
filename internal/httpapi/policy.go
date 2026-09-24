package httpapi

import (
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
)

const defaultMaxRequestBodyBytes = 1 << 20 // 1 MiB

// ProtectedOperations 为业务模块声明统一的认证、请求限制和错误契约。
func ProtectedOperations(tag string, extraErrors ...int) func(huma.Operation) Operation {
	return func(spec huma.Operation) Operation {
		spec.Tags = []string{tag}
		spec.Errors = append([]int{
			http.StatusBadRequest,
			http.StatusUnauthorized,
			http.StatusNotFound,
			http.StatusRequestEntityTooLarge,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout,
		}, extraErrors...)
		spec.MaxBodyBytes = defaultMaxRequestBodyBytes
		return NewOperation(Session, spec)
	}
}

// MethodNotAllowed 在替换 Chi 默认响应体时保留 Allow 响应头。
func MethodNotAllowed(router chi.Routes) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var allowed []string
		path := r.URL.RawPath
		if path == "" {
			path = r.URL.Path
		}
		for _, method := range []string{http.MethodConnect, http.MethodDelete, http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPatch, http.MethodPost, http.MethodPut, http.MethodTrace} {
			if router.Match(chi.NewRouteContext(), method, path) {
				allowed = append(allowed, method)
			}
		}
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		Problem(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
