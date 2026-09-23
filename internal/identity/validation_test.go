package identity

import (
	"strings"
	"testing"
)

func TestSubjectValidation(t *testing.T) {
	for _, tc := range []struct {
		subject string
		valid   bool
	}{
		{"alice", true}, {strings.Repeat("中", 85), true}, {strings.Repeat("中", 86), false},
		{" \t\n", false}, {"", false}, {"a\x00", false}, {string([]byte{255}), false},
	} {
		if (ValidateSubject(tc.subject) == nil) != tc.valid {
			t.Fatal("主体格式校验错误")
		}
		if (RequireSubject(tc.subject) == nil) != tc.valid {
			t.Fatal("服务入口校验错误")
		}
	}
}
