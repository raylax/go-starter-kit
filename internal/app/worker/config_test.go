package worker

import "testing"

func TestConfigIndependentOfAPI(t *testing.T) {
	_, err := Parse(func(k string) (string, bool) {
		switch k {
		case "DATABASE_URL":
			return "postgres://localhost/test", true
		case "HTTP_ADDR", "AUTH_PROVIDERS_FILE", "LOCK_DATABASE_URL":
			return "invalid-api-setting", true
		default:
			return "", false
		}
	})
	if err != nil {
		t.Fatalf("独立配置失败: %v", err)
	}
	for _, key := range []string{"LOG_LEVEL", "SHUTDOWN_TIMEOUT", "OTEL_ENABLED"} {
		t.Run(key, func(t *testing.T) {
			_, err := Parse(func(k string) (string, bool) {
				if k == key {
					return "", true
				}
				if k == "DATABASE_URL" {
					return "postgres://localhost/test", true
				}
				return "", false
			})
			if err == nil {
				t.Fatal("未拒绝无效配置")
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
				"DATABASE_URL": "postgres://localhost/test"}
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
