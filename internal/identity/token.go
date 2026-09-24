package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

const tokenEntropyBytes = 32

// NewToken 生成有独立类型的高熵随机令牌，数据库仅保存摘要。
func NewToken(prefix string) (string, []byte) {
	raw := prefix + base64.RawURLEncoding.EncodeToString(randomBytes(tokenEntropyBytes))
	return raw, Digest(raw)
}

func randomBytes(n int) []byte { b := make([]byte, n); _, _ = rand.Read(b); return b }

func Digest(raw string) []byte { sum := sha256.Sum256([]byte(raw)); return sum[:] }

func TokenDigest(raw, prefix string) ([]byte, error) {
	if len(raw) != len(prefix)+base64.RawURLEncoding.EncodedLen(tokenEntropyBytes) || !strings.HasPrefix(raw, prefix) {
		return nil, ErrUnauthorized
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw[len(prefix):])
	if err != nil || len(decoded) != tokenEntropyBytes {
		return nil, ErrUnauthorized
	}
	return Digest(raw), nil
}
