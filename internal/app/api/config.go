package api

import (
	"fmt"
	"github.com/example/go-starter-kit/internal/platform/configenv"
	"log/slog"
	"net"
	"net/url"
	"os"
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
	FrontendURL        string        `env:"FRONTEND_URL"`
	OTelEnabled        bool          `env:"OTEL_ENABLED" envDefault:"false"`
	OTelServiceName    string        `env:"OTEL_SERVICE_NAME"`
}

type durationSetting struct {
	key   string
	value time.Duration
}

func Load() (Config, error) { return Parse(os.LookupEnv) }

// Parse 转换环境变量，之后执行业务校验。
func Parse(lookup func(string) (string, bool)) (Config, error) {
	c, err := configenv.Parse[Config](lookup)
	if err != nil {
		return c, err
	}
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
	if err := c.infrastructure().Validate(); err != nil {
		return err
	}

	for _, entry := range []durationSetting{
		{key: "REQUEST_TIMEOUT", value: c.RequestTimeout},
		{key: "AUTH_SESSION_IDLE_TTL", value: c.AuthSessionIdleTTL},
		{key: "AUTH_SESSION_MAX_TTL", value: c.AuthSessionMaxTTL},
	} {
		if entry.value <= 0 {
			return fmt.Errorf("%s 必须为正时长", entry.key)
		}
	}
	if c.FrontendURL == "" {
		return fmt.Errorf("需要前端地址")
	}
	if c.AuthSessionIdleTTL < time.Minute || c.AuthSessionMaxTTL < c.AuthSessionIdleTTL {
		return fmt.Errorf("会话期限配置无效")
	}
	frontend, err := url.Parse(c.FrontendURL)
	if err != nil || frontend.Host == "" || (frontend.Scheme != "http" && frontend.Scheme != "https") {
		return fmt.Errorf("FRONTEND_URL 必须为 HTTP(S) URL")
	}
	return nil
}
