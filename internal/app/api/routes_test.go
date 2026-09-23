package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestOfflineContractMatchesRuntime(t *testing.T) {
	// 无效环境证明离线导出不读取运行配置或初始化认证依赖。
	t.Setenv("DATABASE_URL", "invalid")
	t.Setenv("AUTH_PROVIDERS_FILE", "invalid")
	offline, err := OpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	deps := testDependencies()
	for _, docs := range []bool{true, false} {
		_, api, err := NewHandler(Config{RequestTimeout: time.Second, DocsEnabled: docs}, slog.New(slog.NewTextHandler(io.Discard, nil)), deps)
		if err != nil {
			t.Fatal(err)
		}
		runtime, err := json.MarshalIndent(api.OpenAPI(), "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(offline, runtime) {
			t.Fatalf("离线与运行时契约不一致：docs=%v", docs)
		}
	}
}

func TestHandlerRejectsMissingDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, missing := range []string{"logger", "projects", "tasks", "accounts", "authenticator", "ready"} {
		t.Run(missing, func(t *testing.T) {
			deps := testDependencies()
			log := logger
			switch missing {
			case "logger":
				log = nil
			case "projects":
				deps.Projects = nil
			case "tasks":
				deps.Tasks = nil
			case "accounts":
				deps.Accounts = nil
			case "authenticator":
				deps.Authenticator = nil
			case "ready":
				deps.Ready = nil
			}
			if _, _, err := NewHandler(Config{RequestTimeout: time.Second}, log, deps); err == nil {
				t.Fatal("缺少必要依赖时应立即失败")
			}
		})
	}
}
