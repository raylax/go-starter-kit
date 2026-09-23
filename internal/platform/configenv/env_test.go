package configenv

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultsEmptyAndSanitizedErrors(t *testing.T) {
	type config struct {
		Timeout time.Duration `env:"TIMEOUT" envDefault:"10s"`
		Secret  string        `env:"SECRET"`
	}
	cfg, err := Parse[config](func(string) (string, bool) { return "", false })
	if err != nil || cfg.Timeout != 10*time.Second {
		t.Fatalf("默认值解析失败: %v", err)
	}
	_, err = Parse[config](func(key string) (string, bool) { return "", key == "TIMEOUT" })
	if err == nil || !strings.Contains(err.Error(), "TIMEOUT") {
		t.Fatal("显式空值未被拒绝")
	}
	_, err = Parse[config](func(key string) (string, bool) { return "private-value", key == "TIMEOUT" })
	if err == nil || strings.Contains(err.Error(), "private-value") {
		t.Fatal("解析错误泄露原始值")
	}
	t.Setenv("SECRET", "process-secret")
	cfg, err = Parse[config](func(string) (string, bool) { return "", false })
	if err != nil || cfg.Secret != "" {
		t.Fatal("解析回退读取了进程环境")
	}
}
