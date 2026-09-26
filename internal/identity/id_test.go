package identity

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewID(t *testing.T) {
	first, second := NewID(), NewID()
	if first == second {
		t.Fatal("连续生成的标识重复")
	}
	for _, value := range []string{first, second} {
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.Version() != 4 || ValidateSubject(value) != nil {
			t.Fatal("当前默认生成器应返回符合标识规则的 UUID v4 字符串")
		}
	}
}
