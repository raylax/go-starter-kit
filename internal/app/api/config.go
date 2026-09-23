package api

import (
	"fmt"
	"github.com/example/go-starter-kit/internal/platform/configenv"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	Environment        string        `env:"APP_ENV" envDefault:"production"`
	HTTPAddr           string        `env:"HTTP_ADDR" envDefault:"127.0.0.1:8080"`
	DatabaseURL        string        `env:"DATABASE_URL,required,notEmpty"`
	DBMaxConns         int32         `env:"DB_MAX_CONNS" envDefault:"10"`
	LogLevel           slog.Level    `env:"LOG_LEVEL" envDefault:"info"`
	RequestTimeout     time.Duration `env:"REQUEST_TIMEOUT" envDefault:"15s"`
	ShutdownTimeout    time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`
	DocsEnabled        bool          `env:"DOCS_ENABLED" envDefault:"false"`
	AuthProvidersFile  string        `env:"AUTH_PROVIDERS_FILE"`
	AuthSessionIdleTTL time.Duration `env:"AUTH_SESSION_IDLE_TTL" envDefault:"30m"`
	AuthSessionMaxTTL  time.Duration `env:"AUTH_SESSION_MAX_TTL" envDefault:"24h"`
	AllowedOrigins     []string      `env:"ALLOWED_ORIGINS"`
	FrontendURL        string        `env:"FRONTEND_URL"`
	OTelEnabled        bool          `env:"OTEL_ENABLED" envDefault:"false"`
	OTelServiceName    string        `env:"OTEL_SERVICE_NAME"`
}

func Load() (Config, error) { return Parse(os.LookupEnv) }

// Parse 转换环境变量并规范化 origin，之后执行业务校验。
func Parse(lookup func(string) (string, bool)) (Config, error) {
	c, err := configenv.Parse[Config](lookup)
	if err != nil {
		return c, err
	}
	origins := make([]string, 0, len(c.AllowedOrigins))
	for _, origin := range c.AllowedOrigins {
		if origin = strings.TrimSpace(origin); origin != "" {
			origins = append(origins, origin)
		}
	}
	c.AllowedOrigins = origins
	return c, c.Validate()
}

// Validate 供解析与运行入口共用，不读取环境或初始化外部依赖。
func (c Config) Validate() error {
	if c.Environment != "production" && c.Environment != "development" && c.Environment != "test" {
		return fmt.Errorf("APP_ENV must be production, development or test")
	}
	if _, _, err := net.SplitHostPort(c.HTTPAddr); err != nil {
		return fmt.Errorf("HTTP_ADDR must be host:port")
	}
	if err := validateDatabaseURL(c.DatabaseURL); err != nil {
		return err
	}

	if c.DBMaxConns < 1 {
		return fmt.Errorf("DB_MAX_CONNS 必须为正整数")
	}
	for _, entry := range []struct {
		key   string
		value time.Duration
	}{
		{"REQUEST_TIMEOUT", c.RequestTimeout}, {"SHUTDOWN_TIMEOUT", c.ShutdownTimeout},
		{"AUTH_SESSION_IDLE_TTL", c.AuthSessionIdleTTL}, {"AUTH_SESSION_MAX_TTL", c.AuthSessionMaxTTL},
	} {
		if entry.value <= 0 {
			return fmt.Errorf("%s 必须为正时长", entry.key)
		}
	}
	if c.OTelEnabled && strings.TrimSpace(c.OTelServiceName) == "" {
		return fmt.Errorf("启用遥测时必须显式设置非空的 OTEL_SERVICE_NAME")
	}
	if c.FrontendURL == "" {
		return fmt.Errorf("需要前端地址")
	}
	if c.AuthSessionIdleTTL < time.Minute || c.AuthSessionMaxTTL < c.AuthSessionIdleTTL {
		return fmt.Errorf("会话期限配置无效")
	}
	if c.FrontendURL != "" && !validFrontend(c.FrontendURL) {
		return fmt.Errorf("FRONTEND_URL 必须为受信前端 origin")
	}
	for _, origin := range c.AllowedOrigins {
		if !validFrontend(origin) {
			return fmt.Errorf("ALLOWED_ORIGINS 包含无效 origin")
		}
	}
	found := false
	for _, origin := range c.AllowedOrigins {
		if origin == c.FrontendURL {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("前端地址必须包含在 ALLOWED_ORIGINS 中")
	}
	return nil
}

func validFrontend(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Path != "" {
		return false
	}
	return u.Scheme == "https"
}

func validateDatabaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return fmt.Errorf("DATABASE_URL 必须为 PostgreSQL URL")
	}
	return nil
}
