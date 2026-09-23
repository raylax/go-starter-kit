package worker

import (
	"bytes"
	"testing"
)

func TestHelpDoesNotLoadConfiguration(t *testing.T) {
	t.Setenv("SHUTDOWN_TIMEOUT", "invalid")
	cmd := NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if out.Len() == 0 {
		t.Fatal("缺少帮助信息")
	}
}
