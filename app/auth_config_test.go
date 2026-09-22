package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/example/go-starter-kit/identity"
)

func TestDevelopmentValidationAtEntryPoints(t *testing.T) {
	for _, tc := range []struct {
		name, environment, token, subject string
		want                              error
		forbidden                         bool
	}{
		{"合法开发配置", "development", "local-development-token", "alice", nil, false},
		{"合法测试配置", "test", "local-development-token", strings.Repeat("中", 85), nil, false},
		{"生产禁用", "production", "local-development-token", "alice", nil, true},
		{"短令牌", "development", "short", "alice", identity.ErrInvalidDevelopmentToken, false},
		{"空用户", "development", "local-development-token", " ", identity.ErrInvalidSubject, false},
		{"用户字节数超限", "development", "local-development-token", strings.Repeat("中", 86), identity.ErrInvalidSubject, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{"DATABASE_URL": "postgres://localhost/test", "APP_ENV": tc.environment, "AUTH_MODE": "dev", "DEV_AUTH_TOKEN": tc.token, "DEV_AUTH_SUBJECT": tc.subject}
			_, parseErr := Parse(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
			_, wireErr := newAuthenticator(t.Context(), Config{Environment: tc.environment, AuthMode: "dev", DevToken: tc.token, DevSubject: tc.subject})
			if tc.forbidden {
				if parseErr == nil || wireErr == nil {
					t.Fatal("某个入口绕过了生产环境限制")
				}
			} else if !errors.Is(parseErr, tc.want) || !errors.Is(wireErr, tc.want) {
				t.Fatalf("入口校验规则不一致：%v / %v", parseErr, wireErr)
			}
			if tc.want != nil {
				field := "DEV_AUTH_SUBJECT"
				if tc.want == identity.ErrInvalidDevelopmentToken {
					field = "DEV_AUTH_TOKEN"
				}
				if !strings.Contains(parseErr.Error(), field) {
					t.Fatal("配置错误缺少具体环境变量名称")
				}
			}
		})
	}
}
