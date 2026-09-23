package password

import (
	"context"
	"github.com/alexedwards/argon2id"
	"strings"
	"testing"
)

func TestPasswordHash(t *testing.T) {
	h, e := New(1)
	if e != nil {
		t.Fatal(e)
	}
	value := "a long password phrase 测试"
	hash, e := h.Hash(t.Context(), value)
	if e != nil {
		t.Fatal(e)
	}
	ok, upgrade, e := h.Verify(t.Context(), hash, value)
	if e != nil || !ok || upgrade {
		t.Fatal("无法验证密码")
	}
	for _, v := range []string{"wrong", value + " "} {
		if ok, _, _ := h.Verify(t.Context(), hash, v); ok {
			t.Fatal("错误密码被接受")
		}
	}
	if ok, _, _ := h.Verify(t.Context(), strings.Replace(hash, "m=19456", "m=999999999", 1), value); ok {
		t.Fatal("接受超大参数")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	h.slots <- struct{}{}
	if _, e := h.Hash(ctx, value); e == nil {
		t.Fatal("未响应取消")
	}
	<-h.slots
}
func TestPasswordPolicy(t *testing.T) {
	for _, v := range []string{"short", strings.Repeat("长", 129), string([]byte{0xff})} {
		if Validate(v) {
			t.Fatal("不符合长度或编码要求的密码被接受")
		}
	}
	for _, v := range []string{"long pass phrase with spaces", strings.Repeat("a", 20), "passwordpassword", "123456789012345"} {
		if !Validate(v) {
			t.Fatal("符合长度和编码要求的密码被拒绝")
		}
	}
}

func TestHashParameterPolicy(t *testing.T) {
	h, err := New(1)
	if err != nil {
		t.Fatal(err)
	}
	const value = "a valid password phrase"
	hash, err := argon2id.CreateHash(value, &argon2id.Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	if err != nil {
		t.Fatal(err)
	}
	matched, upgrade, err := h.Verify(t.Context(), hash, value)
	if err != nil || !matched || !upgrade {
		t.Fatal("未识别需要升级的哈希")
	}
	for _, encoded := range []string{
		"", strings.Repeat("x", maxEncodedHashBytes+1),
		strings.Replace(hash, "t=1", "t=0", 1),
		strings.Replace(hash, "p=1", "p=0", 1),
		strings.Replace(hash, "m=8192", "m=999999999", 1),
	} {
		matched, upgrade, err := h.Verify(t.Context(), encoded, "dummy password used only for timing")
		if err != nil || matched || upgrade {
			t.Fatal("非法参数或 dummy 路径接受了密码")
		}
	}
}
