//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/example/go-starter-kit/internal/app"
	"github.com/example/go-starter-kit/internal/testutil"
	"github.com/example/go-starter-kit/internal/testutil/jwtfixture"
)

func TestServerLifecycle(t *testing.T) {
	_, databaseURL := testutil.Database(t)
	key := jwtfixture.NewKey(t)
	for _, startup := range []bool{true, false} {
		t.Run(fmt.Sprintf("signal_during_startup_%v", startup), func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			var requests atomic.Int32
			jwks := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				if (startup && n == 1) || (!startup && n == 2) {
					close(entered)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				w.Header().Set("Content-Type", "application/json")
				if !startup && n == 1 {
					_, _ = w.Write([]byte(`{"keys":[]}`))
					return
				}
				_ = json.NewEncoder(w).Encode(jwtfixture.JWKS(&key.PublicKey, "new-key"))
			}))
			defer jwks.Close()
			defer unblock()
			oldTransport := http.DefaultTransport
			http.DefaultTransport = jwks.Client().Transport
			defer func() { http.DefaultTransport = oldTransport }()
			oldLogger := slog.Default()
			defer slog.SetDefault(oldLogger)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			addr := listener.Addr().String()
			_ = listener.Close()
			for k, v := range map[string]string{"APP_ENV": "production", "AUTH_MODE": "jwt", "DATABASE_URL": databaseURL, "AUTH_ISSUER": jwks.URL, "AUTH_JWKS_URL": jwks.URL, "AUTH_AUDIENCE": "lifecycle-api", "HTTP_ADDR": addr, "SHUTDOWN_TIMEOUT": "2s", "REQUEST_TIMEOUT": "5s", "OTEL_ENABLED": "false"} {
				t.Setenv(k, v)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			finished := make(chan struct{})
			go func() { defer close(finished); done <- app.Run(ctx) }()
			defer func() {
				cancel()
				unblock()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Error("server did not terminate during test cleanup")
				}
			}()
			client := &http.Client{Timeout: time.Second}
			responseDone := make(chan error, 1)
			if !startup {
				ready := false
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					res, err := client.Get("http://" + addr + "/health/ready")
					if err == nil {
						_, _ = io.Copy(io.Discard, res.Body)
						_ = res.Body.Close()
						if res.StatusCode == 200 {
							ready = true
							break
						}
					}
					select {
					case err := <-done:
						t.Fatalf("server exited before readiness: %v", err)
					case <-time.After(10 * time.Millisecond):
					}
				}
				if !ready {
					t.Fatal("server never became ready")
				}
				raw := jwtfixture.Sign(t, jwt.SigningMethodRS256, jwt.RegisteredClaims{Issuer: jwks.URL, Audience: []string{"lifecycle-api"}, Subject: "alice", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}, "new-key", key)
				go func() {
					req, err := http.NewRequestWithContext(t.Context(), "GET", "http://"+addr+"/v1/tasks", nil)
					if err != nil {
						responseDone <- err
						return
					}
					req.Header.Set("Authorization", "Bearer "+raw)
					res, err := client.Do(req)
					if err != nil {
						responseDone <- err
						return
					}
					defer res.Body.Close()
					_, _ = io.Copy(io.Discard, res.Body)
					if res.StatusCode != 200 {
						responseDone <- fmt.Errorf("in-flight request got %d", res.StatusCode)
						return
					}
					responseDone <- nil
				}()
			}
			select {
			case <-entered:
			case err := <-done:
				t.Fatalf("server exited: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("JWKS request never started")
			}
			cancel()
			if startup {
				select {
				case <-done:
				case <-time.After(time.Second):
					unblock()
					<-done
					t.Fatal("startup ignored cancellation while waiting for JWKS")
				}
			} else {
				select {
				case err := <-done:
					t.Fatalf("server exited before draining: %v", err)
				case <-time.After(50 * time.Millisecond):
				}
				unblock()
				if err := <-responseDone; err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("server did not finish draining")
				}
			}
		})
	}
}
