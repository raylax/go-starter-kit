package worker

import (
	"fmt"
	"github.com/example/go-starter-kit/internal/platform/configenv"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"
)

// Config 包含后台进程与邮件队列配置。
type Config struct {
	DatabaseURL      string        `env:"DATABASE_URL,required,notEmpty"`
	DBMaxConns       int32         `env:"DB_MAX_CONNS" envDefault:"5"`
	MailPollInterval time.Duration `env:"MAIL_POLL_INTERVAL" envDefault:"2s"`
	LogLevel         slog.Level    `env:"LOG_LEVEL" envDefault:"info"`
	ShutdownTimeout  time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`
	OTelEnabled      bool          `env:"OTEL_ENABLED" envDefault:"false"`
	OTelServiceName  string        `env:"OTEL_SERVICE_NAME"`
}

func Load() (Config, error) { return Parse(os.LookupEnv) }

func Parse(lookup func(string) (string, bool)) (Config, error) {
	c, err := configenv.Parse[Config](lookup)
	if err != nil {
		return c, err
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	u, err := url.Parse(c.DatabaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return fmt.Errorf("DATABASE_URL 必须为 PostgreSQL URL")
	}
	if c.DBMaxConns < 1 || c.MailPollInterval <= 0 {
		return fmt.Errorf("数据库连接数和邮件轮询间隔必须为正")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("SHUTDOWN_TIMEOUT 必须为正时长")
	}
	if c.OTelEnabled && strings.TrimSpace(c.OTelServiceName) == "" {
		return fmt.Errorf("启用遥测时必须显式设置非空的 OTEL_SERVICE_NAME")
	}
	return nil
}
