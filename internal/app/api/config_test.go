package api

import "testing"

func TestConfigurationBoundaries(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL": "postgres://user:password@localhost/test",
		"FRONTEND_URL": "https://web.example.com",
	}
	for _, tc := range []struct {
		name   string
		values map[string]string
		valid  bool
	}{
		{"production defaults", nil, true},
		{"providers configured", map[string]string{"AUTH_PROVIDERS_FILE": "/test/providers.json"}, true},
		{"unknown environment", map[string]string{"APP_ENV": "prodution"}, false},
		{"missing database", map[string]string{"DATABASE_URL": ""}, false},
		{"invalid pool", map[string]string{"DB_MAX_CONNS": "0"}, false},
		{"invalid boolean", map[string]string{"DOCS_ENABLED": "yes"}, false},
		{"negative timeout", map[string]string{"REQUEST_TIMEOUT": "-1s"}, false},
		{"HTTP frontend", map[string]string{"FRONTEND_URL": "http://web.example.com"}, true},
		{"explicit default port", map[string]string{"FRONTEND_URL": "https://web.example.com:443"}, true},
		{"invalid frontend URL", map[string]string{"FRONTEND_URL": "invalid"}, false},
		{"invalid expiry", map[string]string{"AUTH_SESSION_IDLE_TTL": "48h"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookup := func(key string) (string, bool) {
				if value, ok := tc.values[key]; ok {
					return value, true
				}
				value, ok := base[key]
				return value, ok
			}
			cfg, err := Parse(lookup)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
			if tc.name == "production defaults" && cfg.DocsEnabled {
				t.Fatal("unsafe production defaults")
			}
		})
	}
}

func TestTelemetryRequiresExplicitServiceName(t *testing.T) {
	for _, tc := range []struct {
		name, enabled, value string
		set, valid           bool
	}{
		{"关闭时未设置", "false", "", false, true},
		{"关闭时空值", "false", "", true, true},
		{"启用时未设置", "true", "", false, false},
		{"启用时空值", "true", "", true, false},
		{"启用时空白", "true", "  ", true, false},
		{"显式服务名", "true", "example-service", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{"OTEL_ENABLED": tc.enabled,
				"DATABASE_URL": "postgres://localhost/test", "APP_ENV": "test",
				"FRONTEND_URL": "https://web.example"}
			if tc.set {
				values["OTEL_SERVICE_NAME"] = tc.value
			}
			_, err := Parse(func(key string) (string, bool) { v, ok := values[key]; return v, ok })
			if (err == nil) != tc.valid {
				t.Fatalf("配置结果不符: %v", err)
			}
		})
	}
}
