package api

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestOfflineCommandsDoNotLoadConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("FRONTEND_URL", "invalid")
	for _, args := range [][]string{{"--help"}, {"serve", "--help"}, {"migrate", "up", "--help"}, {"openapi"}} {
		cmd := NewCommand()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		if err := cmd.ExecuteContext(t.Context()); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if len(args) == 1 && args[0] == "openapi" && !json.Valid(out.Bytes()) {
			t.Fatal("离线契约输出不是 JSON")
		}
	}
}

func TestCommandRejectsExtraArguments(t *testing.T) {
	for _, args := range [][]string{{"unknown"}, {"serve", "extra"}, {"openapi", "extra"}, {"migrate"}, {"migrate", "up", "extra"}, {"--unknown-option"}} {
		cmd := NewCommand()
		cmd.SetArgs(args)
		if err := cmd.ExecuteContext(t.Context()); err == nil {
			t.Fatalf("未拒绝 %v", args)
		}
	}
}
