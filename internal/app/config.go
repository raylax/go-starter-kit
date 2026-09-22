package app

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment     string
	HTTPAddr        string
	DatabaseURL     string
	DBMaxConns      int32
	LogLevel        slog.Level
	RequestTimeout  time.Duration
	ShutdownTimeout time.Duration
	DocsEnabled     bool
	AuthMode        string
	DevToken        string
	DevSubject      string
	AuthIssuer      string
	AuthAudience    string
	AuthJWKSURL     string
	OTelEnabled     bool
	ServiceName     string
}

func Load() (Config, error) { return Parse(os.LookupEnv) }

// Parse 在监听端口或连接外部服务之前完成配置校验。
func Parse(lookup func(string) (string, bool)) (Config, error) {
	get := func(key, fallback string) string {
		if value, ok := lookup(key); ok {
			return value
		}
		return fallback
	}
	c := Config{
		Environment: get("APP_ENV", "production"), HTTPAddr: get("HTTP_ADDR", "127.0.0.1:8080"),
		DatabaseURL: get("DATABASE_URL", ""), AuthMode: get("AUTH_MODE", "jwt"),
		DevToken: get("DEV_AUTH_TOKEN", ""), DevSubject: get("DEV_AUTH_SUBJECT", ""),
		AuthIssuer: get("AUTH_ISSUER", ""), AuthAudience: get("AUTH_AUDIENCE", ""), AuthJWKSURL: get("AUTH_JWKS_URL", ""),
		ServiceName: get("OTEL_SERVICE_NAME", "go-starter-kit"),
	}
	if c.Environment != "production" && c.Environment != "development" && c.Environment != "test" {
		return c, fmt.Errorf("APP_ENV must be production, development or test")
	}
	if _, _, err := net.SplitHostPort(c.HTTPAddr); err != nil {
		return c, fmt.Errorf("HTTP_ADDR must be host:port")
	}
	databaseURL, err := url.Parse(c.DatabaseURL)
	if err != nil || databaseURL.Host == "" || (databaseURL.Scheme != "postgres" && databaseURL.Scheme != "postgresql") {
		return c, fmt.Errorf("DATABASE_URL must be a PostgreSQL URL")
	}
	maxConns, err := strconv.ParseInt(get("DB_MAX_CONNS", "10"), 10, 32)
	if err != nil || maxConns < 1 {
		return c, fmt.Errorf("DB_MAX_CONNS must be a positive int32")
	}
	c.DBMaxConns = int32(maxConns)
	if err := c.LogLevel.UnmarshalText([]byte(get("LOG_LEVEL", "info"))); err != nil {
		return c, fmt.Errorf("invalid LOG_LEVEL")
	}
	for _, entry := range []struct {
		key, fallback string
		target        *time.Duration
	}{
		{"REQUEST_TIMEOUT", "15s", &c.RequestTimeout}, {"SHUTDOWN_TIMEOUT", "10s", &c.ShutdownTimeout},
	} {
		value, err := time.ParseDuration(get(entry.key, entry.fallback))
		if err != nil || value <= 0 {
			return c, fmt.Errorf("%s must be a positive duration", entry.key)
		}
		*entry.target = value
	}
	for _, entry := range []struct {
		key    string
		target *bool
	}{
		{"DOCS_ENABLED", &c.DocsEnabled}, {"OTEL_ENABLED", &c.OTelEnabled},
	} {
		value, err := strconv.ParseBool(get(entry.key, "false"))
		if err != nil {
			return c, fmt.Errorf("%s must be a boolean", entry.key)
		}
		*entry.target = value
	}
	if strings.TrimSpace(c.ServiceName) == "" {
		return c, fmt.Errorf("OTEL_SERVICE_NAME is required")
	}
	switch c.AuthMode {
	case "dev":
		if err := validateAuthEnvironment(c.Environment, c.AuthMode); err != nil {
			return c, err
		}
		if err := validateDevelopmentCredentials(c.DevToken, c.DevSubject); err != nil {
			return c, err
		}
	case "jwt":
		for key, value := range map[string]string{"AUTH_ISSUER": c.AuthIssuer, "AUTH_JWKS_URL": c.AuthJWKSURL} {
			u, err := url.Parse(value)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
				return c, fmt.Errorf("%s must be an HTTPS URL", key)
			}
		}
		if strings.TrimSpace(c.AuthAudience) == "" {
			return c, fmt.Errorf("AUTH_AUDIENCE is required")
		}
	default:
		return c, fmt.Errorf("AUTH_MODE must be jwt or dev")
	}
	return c, nil
}
