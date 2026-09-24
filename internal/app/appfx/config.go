package appfx

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL     string
	DBMaxConns      int32
	LogLevel        slog.Level
	OTelEnabled     bool
	OTelServiceName string
	ShutdownTimeout time.Duration
}

// Validate 维护所有进程共用的基础设施配置规则。
func (c Config) Validate() error {
	if err := ValidateDatabaseURL(c.DatabaseURL); err != nil {
		return err
	}
	if c.DBMaxConns < 1 {
		return fmt.Errorf("DB_MAX_CONNS 必须为正整数")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("SHUTDOWN_TIMEOUT 必须为正时长")
	}
	if c.OTelEnabled && strings.TrimSpace(c.OTelServiceName) == "" {
		return fmt.Errorf("启用遥测时必须显式设置非空的 OTEL_SERVICE_NAME")
	}
	return nil
}

// ValidateDatabaseURL 供独立迁移命令复用，不要求运行时的其他配置。
func ValidateDatabaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return fmt.Errorf("DATABASE_URL 必须为 PostgreSQL URL")
	}
	return nil
}
