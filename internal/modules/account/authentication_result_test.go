package account

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCallbackOutputPreservesVariantContract(t *testing.T) {
	id := uuid.New()
	expiresAt := time.Now().Add(time.Hour)
	cases := []struct {
		name   string
		result CallbackResult
		keys   []string
	}{
		{
			name: "登录会话",
			result: sessionCallback(SessionCredentials{
				ID: id, Token: "tk_test", IdleExpiresAt: expiresAt, AbsoluteExpiresAt: expiresAt,
			}),
			keys: []string{"result", "session"},
		},
		{name: "操作证明", result: reauthenticatedCallback(id), keys: []string{"result", "reauthentication_id"}},
		{name: "待确认绑定", result: linkPendingCallback(LinkConfirmation{FlowID: id, Provider: "github"}), keys: []string{"result", "flow_id", "provider", "name"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output, err := callbackOutput(tc.result)
			if err != nil {
				t.Fatal(err)
			}
			if output.CacheControl != "no-store" || output.Body.Result != tc.result.Result() {
				t.Fatal("回调响应的缓存或结果类型不匹配")
			}
			assertAuthenticationBodyKeys(t, output.Body, tc.keys)
		})
	}
}

func TestReauthenticateOutputPreservesVariantContract(t *testing.T) {
	id := uuid.New()
	cases := []struct {
		name   string
		result ReauthenticationResult
		keys   []string
	}{
		{
			name: "认证跳转",
			result: redirectReauthentication(FlowResult{
				ID: id, Token: "flow_test", AuthorizationURL: "https://example.com/oauth", ExpiresAt: time.Now().Add(time.Minute),
			}),
			keys: []string{"result", "flow"},
		},
		{name: "操作证明", result: completedReauthentication(id), keys: []string{"result", "reauthentication_id"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output, err := reauthenticateOutput(tc.result)
			if err != nil {
				t.Fatal(err)
			}
			if output.CacheControl != "no-store" || output.Body.Result != tc.result.Result() {
				t.Fatal("重新认证响应的缓存或结果类型不匹配")
			}
			assertAuthenticationBodyKeys(t, output.Body, tc.keys)
		})
	}
}

func TestAuthenticationOutputRejectsInvalidState(t *testing.T) {
	for name, result := range map[string]CallbackResult{
		"回调零值":    {},
		"回调未知结果":  {result: AuthResult("future")},
		"回调不支持跳转": {result: ResultRedirect},
		"回调缺少会话":  sessionCallback(SessionCredentials{}),
		"回调缺少证明":  reauthenticatedCallback(uuid.Nil),
		"回调缺少绑定":  linkPendingCallback(LinkConfirmation{}),
	} {
		t.Run(name, func(t *testing.T) {
			output, err := callbackOutput(result)
			if output != nil || !errors.Is(err, errInvalidAuthenticationResult) {
				t.Fatal("非法回调结果必须返回内部错误")
			}
		})
	}
	for name, result := range map[string]ReauthenticationResult{
		"重新认证零值":    {},
		"重新认证未知结果":  {result: AuthResult("future")},
		"重新认证不支持会话": {result: ResultSession},
		"重新认证缺少跳转":  redirectReauthentication(FlowResult{}),
		"重新认证缺少证明":  completedReauthentication(uuid.Nil),
	} {
		t.Run(name, func(t *testing.T) {
			output, err := reauthenticateOutput(result)
			if output != nil || !errors.Is(err, errInvalidAuthenticationResult) {
				t.Fatal("非法重新认证结果必须返回内部错误")
			}
		})
	}
}

func assertAuthenticationBodyKeys(t *testing.T, body any, want []string) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(fields))
	for key := range fields {
		got = append(got, key)
	}
	slices.Sort(got)
	want = slices.Clone(want)
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("响应字段=%v，期望=%v", got, want)
	}
}
