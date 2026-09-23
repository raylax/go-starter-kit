// Package httpapi 提供与具体业务无关的 HTTP 能力。
package httpapi

import (
	"log/slog"
	"net/http"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Config struct {
	RequestTimeout time.Duration
	DocsEnabled    bool
	AllowedOrigins []string
}

// New 创建基础路由器，具体业务路由由应用装配层注册。
func New(cfg Config, logger *slog.Logger) (http.Handler, huma.API) {
	router := chi.NewRouter()
	router.Use(Requests(logger, cfg.RequestTimeout))
	router.Use(CORS(cfg.AllowedOrigins))
	router.NotFound(func(w http.ResponseWriter, _ *http.Request) { Problem(w, http.StatusNotFound, "route not found") })
	router.MethodNotAllowed(MethodNotAllowed(router))
	apiConfig := huma.DefaultConfig("Go Starter Kit API", "1.0.0")
	// 只调整第三方错误模型的公开名称，业务 schema 直接使用具名 DTO。
	apiConfig.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", func(t reflect.Type, hint string) string {
		if t == reflect.TypeFor[huma.ErrorModel]() || t == reflect.TypeFor[*huma.ErrorModel]() {
			return "ErrorResponse"
		}
		return huma.DefaultSchemaNamer(t, hint)
	})
	apiConfig.RejectUnknownQueryParameters = true
	apiConfig.Transformers = append(apiConfig.Transformers, sanitizeErrors)
	// 响应结构不依赖文档地址，同时禁用自动添加的 $schema 字段。
	apiConfig.CreateHooks = nil
	apiConfig.SchemasPath = ""
	if !cfg.DocsEnabled {
		apiConfig.OpenAPIPath, apiConfig.DocsPath = "", ""
	}
	apiConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearer": {Type: "http", Scheme: "bearer", Description: "以 tk_ 开头的用户会话令牌。"},
	}
	api := humachi.New(router, apiConfig)
	return otelhttp.NewHandler(router, "http.server"), api
}
