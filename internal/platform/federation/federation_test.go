package federation

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestVerifyClassifiesFailuresWithoutExposingProofs(t *testing.T) {
	const secret = "private-provider-response"
	for _, tc := range []struct {
		name, stage, body string
		status            int
		network           bool
		want              error
	}{
		{name: "交换网络故障", stage: "token", network: true, want: ErrUnavailable},
		{name: "交换服务故障", stage: "token", status: http.StatusServiceUnavailable, body: secret, want: ErrUnavailable},
		{name: "交换限流", stage: "token", status: http.StatusTooManyRequests, body: secret, want: ErrUnavailable},
		{name: "无效授权码", stage: "token", status: http.StatusBadRequest, body: `{"error":"invalid_grant","error_description":"` + secret + `"}`, want: ErrProof},
		{name: "GitHub 无效授权码", stage: "token", status: http.StatusOK, body: `{"error":"bad_verification_code","error_description":"` + secret + `"}`, want: ErrProof},
		{name: "客户端配置错误", stage: "token", status: http.StatusUnauthorized, body: `{"error":"invalid_client","error_description":"` + secret + `"}`, want: ErrUnavailable},
		{name: "用户服务故障", stage: "user", status: http.StatusServiceUnavailable, body: secret, want: ErrUnavailable},
		{name: "用户限流", stage: "user", status: http.StatusForbidden, body: secret, want: ErrUnavailable},
		{name: "用户凭据失效", stage: "user", status: http.StatusUnauthorized, body: secret, want: ErrProof},
		{name: "用户响应损坏", stage: "user", status: http.StatusOK, body: secret, want: ErrUnavailable},
		{name: "成功", stage: "user", status: http.StatusOK, body: `{"id":123,"login":"tester"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := New([]Config{{ID: "github", Protocol: ProtocolGitHub, ClientID: "test", ClientSecret: "test-only", RedirectURI: "https://example.com/auth/callback"}})
			if err != nil {
				t.Fatal(err)
			}
			r.client.Transport = testTransport(func(req *http.Request) (*http.Response, error) {
				status, body := tc.status, tc.body
				if tc.stage == "user" && req.URL.Host == "github.com" {
					status = http.StatusOK
					body = `{"access_token":"test-only","token_type":"bearer"}`
				} else if tc.network {
					return nil, errors.New(secret)
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			verified, err := r.Verify(t.Context(), "github", r.Version("github"), "test-code", "test-verifier")
			if !errors.Is(err, tc.want) {
				t.Fatalf("错误分类: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), secret) {
				t.Fatal("错误回显第三方正文")
			}
			if err == nil && verified.Subject != "123" {
				t.Fatal("身份转换错误")
			}
		})
	}
}

func TestExchangePreservesRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := exchangeError(ctx, errors.New("private")); !errors.Is(err, context.Canceled) {
		t.Fatalf("请求取消丢失: %v", err)
	}
	ctx, stop := context.WithTimeout(t.Context(), 0)
	defer stop()
	if err := dependencyError(ctx, errors.New("private")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("请求超时丢失: %v", err)
	}
}
