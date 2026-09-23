package identity

import (
	"bytes"
	"strings"
	"testing"
)

func TestOpaqueToken(t *testing.T) {
	token, hash := NewToken("tk_")
	other, _ := NewToken("tk_")
	if token == other || !strings.HasPrefix(token, "tk_") || len(hash) != 32 {
		t.Fatal("令牌随机性或格式错误")
	}
	got, e := TokenDigest(token, "tk_")
	if e != nil || !bytes.Equal(hash, got) {
		t.Fatal("令牌无法匹配摘要")
	}
	for _, bad := range []string{"", token + "=", strings.Replace(token, "tk_", "flow_", 1), "tk_" + strings.Repeat("!", 43), "tk_" + strings.Repeat("a", 42)} {
		if _, e := TokenDigest(bad, "tk_"); e == nil {
			t.Fatal("接受了错误格式")
		}
	}
}
