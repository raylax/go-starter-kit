package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/example/go-starter-kit/tests/testutil/jwtfixture"
)

func TestJWTWithRemoteJWKS(t *testing.T) {
	key := jwtfixture.NewKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwtfixture.JWKS(&key.PublicKey, "test-key"))
	}))
	defer server.Close()
	authenticator, err := NewJWT(t.Context(), JWTConfig{Issuer: "https://issuer.example/", Audience: "api", JWKSURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*jwt.RegisteredClaims)
		method jwt.SigningMethod
		valid  bool
	}{
		{"valid", nil, jwt.SigningMethodRS256, true},
		{"wrong issuer", func(c *jwt.RegisteredClaims) { c.Issuer = "https://other.example/" }, jwt.SigningMethodRS256, false},
		{"wrong audience", func(c *jwt.RegisteredClaims) { c.Audience = []string{"web-client"} }, jwt.SigningMethodRS256, false},
		{"expired", func(c *jwt.RegisteredClaims) { c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute)) }, jwt.SigningMethodRS256, false},
		{"missing expiry", func(c *jwt.RegisteredClaims) { c.ExpiresAt = nil }, jwt.SigningMethodRS256, false},
		{"future nbf", func(c *jwt.RegisteredClaims) { c.NotBefore = jwt.NewNumericDate(time.Now().Add(time.Hour)) }, jwt.SigningMethodRS256, false},
		{"missing subject", func(c *jwt.RegisteredClaims) { c.Subject = "" }, jwt.SigningMethodRS256, false},
		{"algorithm confusion", nil, jwt.SigningMethodHS256, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := jwt.RegisteredClaims{Issuer: "https://issuer.example/", Audience: []string{"api"}, Subject: "alice", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}
			if tc.mutate != nil {
				tc.mutate(&claims)
			}
			var signingKey any = key
			if tc.method == jwt.SigningMethodHS256 {
				signingKey = []byte("invalid-shared-key")
			}
			raw := jwtfixture.Sign(t, tc.method, claims, "test-key", signingKey)
			subject, err := authenticator.Authenticate(t.Context(), raw)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if tc.valid && subject != "alice" {
				t.Fatalf("unexpected subject %q", subject)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := authenticator.Authenticate(ctx, "token"); err == nil {
		t.Fatal("canceled request accepted")
	}
}

func TestJWTRefreshFailuresPreserveCause(t *testing.T) {
	key := jwtfixture.NewKey(t)
	for _, mode := range []string{"timeout", "unavailable", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if requests.Add(1) > 1 {
					switch mode {
					case "timeout":
						<-r.Context().Done()
						return
					case "unavailable":
						w.WriteHeader(503)
						return
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"keys":[]}`))
			}))
			defer server.Close()
			a, err := NewJWT(t.Context(), JWTConfig{Issuer: "https://issuer.example/", Audience: "api", JWKSURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			raw := jwtfixture.Sign(t, jwt.SigningMethodRS256, jwt.RegisteredClaims{Subject: "alice", Issuer: "https://issuer.example/", Audience: []string{"api"}, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}, "rotated-key", key)
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			_, err = a.Authenticate(ctx, raw)
			switch mode {
			case "timeout":
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("lost timeout cause: %v", err)
				}
			case "unavailable":
				if err == nil || errors.Is(err, ErrUnauthorized) {
					t.Fatalf("dependency failure treated as invalid token: %v", err)
				}
				// 立即重试会触发 JWKS 刷新限流，仍应归类为依赖故障。
				_, err = a.Authenticate(ctx, raw)
				if !errors.Is(err, ErrUnavailable) {
					t.Fatalf("refresh backoff lost dependency cause: %v", err)
				}
			case "unknown":
				if !errors.Is(err, ErrUnauthorized) {
					t.Fatalf("unknown key is not an auth failure: %v", err)
				}
			}
		})
	}
}

func TestDevelopmentAuthentication(t *testing.T) {
	authenticator := development{token: "local-development-token", subject: "alice"}
	for _, header := range []string{"", "Bearer", "Basic local-development-token", "Bearer wrong", "Bearer local-development-token extra"} {
		if _, err := authenticator.Authenticate(t.Context(), header); err == nil {
			t.Fatalf("accepted %q", header)
		}
	}
	if subject, err := authenticator.Authenticate(t.Context(), "local-development-token"); err != nil || subject != "alice" {
		t.Fatalf("subject=%q error=%v", subject, err)
	}
}
