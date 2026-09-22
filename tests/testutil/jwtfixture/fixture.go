// Package jwtfixture 提供独立于应用和认证实现的 JWT 测试夹具。
package jwtfixture

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"math/big"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func NewKey(t testing.TB) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func JWKS(key *rsa.PublicKey, kid string) map[string]any {
	return map[string]any{"keys": []any{map[string]string{
		"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256",
		"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}}}
}

// Sign 由测试显式传入 claims、算法和密钥，保留错误签名等场景的控制权。
func Sign(t testing.TB, method jwt.SigningMethod, claims jwt.Claims, kid string, key any) string {
	t.Helper()
	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = kid
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
