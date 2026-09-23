package httpapi

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type loggerKey struct{}
type requestMetaKey struct{}
type RequestMeta struct{ ID, ClientIP string }

func Metadata(ctx context.Context) RequestMeta {
	m, _ := ctx.Value(requestMetaKey{}).(RequestMeta)
	return m
}

func Logger(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}

// Requests 统一处理请求标识、日志、超时上下文和异常恢复。
func Requests(logger *slog.Logger, timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			requestID := uuid.Must(uuid.NewV7()).String()
			w.Header().Set("X-Request-ID", requestID)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			log := logger.With("request_id", requestID)
			span := trace.SpanFromContext(r.Context())
			if sc := span.SpanContext(); sc.IsValid() {
				log = log.With("trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String())
			}
			ctx, cancel := context.WithTimeout(context.WithValue(r.Context(), loggerKey{}, log), timeout)
			ip, _, _ := net.SplitHostPort(r.RemoteAddr)
			ctx = context.WithValue(ctx, requestMetaKey{}, RequestMeta{ID: requestID, ClientIP: ip})
			defer cancel()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			defer func() {
				if value := recover(); value != nil {
					log.ErrorContext(ctx, "request panic", "panic", value, "stack", string(debug.Stack()))
					if ww.Status() == 0 {
						Problem(ww, http.StatusInternalServerError, "internal server error")
					}
				}
				route := chi.RouteContext(r.Context()).RoutePattern()
				if route == "" {
					route = "unmatched"
				}
				span.SetName(r.Method + " " + route)
				span.SetAttributes(attribute.String("http.route", route))
				if labeler, ok := otelhttp.LabelerFromContext(ctx); ok {
					labeler.Add(attribute.String("http.route", route))
				}
				status := ww.Status()
				if status == 0 {
					status = http.StatusOK
				}
				log.InfoContext(ctx, "http request", "method", r.Method, "route", route, "status", status, "bytes", ww.BytesWritten(), "duration_ms", time.Since(start).Milliseconds())
			}()
			next.ServeHTTP(ww, r.WithContext(ctx))
		})
	}
}
