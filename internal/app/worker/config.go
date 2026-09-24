package worker

import (
	"fmt"
	"github.com/example/go-starter-kit/internal/platform/configenv"
	"log/slog"
	"os"
	"time"
)

// Config 包含后台进程与邮件队列配置。
type Config struct {
	DatabaseURL      string        `env:"DATABASE_URL,required,notEmpty"`
	DBMaxConns       int32         `env:"DB_MAX_CONNS" envDefault:"5"`
	MailPollInterval time.Duration `env:"MAIL_POLL_INTERVAL" envDefault:"1s"`
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
	if err := c.infrastructure().Validate(); err != nil {
		return err
	}
	if c.MailPollInterval <= 0 {
		return fmt.Errorf("MAIL_POLL_INTERVAL 必须为正时长")
	}
	return nil
}
