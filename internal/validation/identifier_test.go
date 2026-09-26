package validation

import (
	"strings"
	"testing"
)

func TestIdentifier(t *testing.T) {
	for _, value := range []string{"user_alice", "01JABC", "账户-一", strings.Repeat("x", MaxIdentifierBytes), "  exact-id  "} {
		if !Identifier(value) {
			t.Fatalf("拒绝合法的不透明标识 %q", value)
		}
	}
	for _, value := range []string{"", " \t", "id\x00x", string([]byte{0xff}), strings.Repeat("x", MaxIdentifierBytes+1), strings.Repeat("中", MaxIdentifierBytes/3+1)} {
		if Identifier(value) {
			t.Fatal("接受了空白、非法编码或超过字节上限的标识")
		}
	}
}
