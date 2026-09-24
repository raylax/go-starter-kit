package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/example/go-starter-kit/internal/apperror"
)

type adapterInput struct {
	Value string `query:"value"`
}
type adapterOutput struct {
	Location string `header:"Location"`
	Body     struct {
		Value string `json:"value"`
	}
}

type adapterContextKey struct{}

func TestMapEndpointExecutionAndPresentation(t *testing.T) {
	failure := apperror.Wrap(apperror.New(apperror.Conflict, "already exists"), errors.New("private storage details"))
	for _, tc := range []struct {
		name                  string
		err                   error
		status, presentations int
	}{
		{"成功后组装响应", nil, http.StatusCreated, 1},
		{"失败时只映射错误", failure, http.StatusConflict, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := chi.NewRouter()
			api := humachi.New(router, huma.DefaultConfig("adapter test", "1"))
			service := new(int)
			calls, presentations := 0, 0
			route := MapEndpoint(NewOperation(Public, huma.Operation{OperationID: "mapped", Method: http.MethodGet, Path: "/mapped", DefaultStatus: http.StatusCreated}),
				func(got *int, ctx context.Context, input *adapterInput) (string, error) {
					calls++
					if got != service || ctx.Value(adapterContextKey{}) != "request-context" || input.Value != "hello" {
						t.Fatal("业务调用未收到绑定的服务、请求上下文或输入")
					}
					return input.Value, tc.err
				},
				func(value string) *adapterOutput {
					presentations++
					out := &adapterOutput{Location: "/items/1"}
					out.Body.Value = value
					return out
				},
			)
			route.Bind(api, service)
			req := httptest.NewRequest(http.MethodGet, "/mapped?value=hello", nil)
			req = req.WithContext(context.WithValue(req.Context(), adapterContextKey{}, "request-context"))
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			if recorder.Code != tc.status || calls != 1 || presentations != tc.presentations {
				t.Fatalf("适配器调用次数或状态错误：status=%d calls=%d presentations=%d", recorder.Code, calls, presentations)
			}
			if tc.err == nil {
				var body struct {
					Value string `json:"value"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Value != "hello" || recorder.Header().Get("Location") != "/items/1" {
					t.Fatal("成功响应正文或响应头丢失")
				}
			} else if recorder.Header().Get("Location") != "" || strings.Contains(recorder.Body.String(), "private storage") || !strings.Contains(recorder.Body.String(), "already exists") {
				t.Fatal("失败路径组装了成功响应或泄露底层原因")
			}
		})
	}
}

func TestNoContentEndpoint(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{nil, http.StatusNoContent}, {apperror.New(apperror.NotFound, "item not found"), http.StatusNotFound},
	} {
		router := chi.NewRouter()
		api := humachi.New(router, huma.DefaultConfig("adapter test", "1"))
		calls := 0
		route := NoContentEndpoint(NewOperation(Public, huma.Operation{OperationID: "delete-item", Method: http.MethodDelete, Path: "/item"}), func(_ struct{}, _ context.Context, _ *struct{}) error { calls++; return tc.err })
		route.Bind(api, struct{}{})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/item", nil))
		if recorder.Code != tc.status || calls != 1 {
			t.Fatalf("删除适配器行为错误：status=%d calls=%d", recorder.Code, calls)
		}
		if tc.err == nil && recorder.Body.Len() != 0 {
			t.Fatal("204 响应不应包含正文")
		}
	}
}

func TestAdapterDescriptionDoesNotExecuteCallbacks(t *testing.T) {
	api := humachi.New(chi.NewRouter(), huma.DefaultConfig("adapter test", "1"))
	mapped := MapEndpoint(NewOperation(Public, huma.Operation{OperationID: "mapped", Method: http.MethodGet, Path: "/mapped"}),
		func(_ struct{}, _ context.Context, _ *adapterInput) (string, error) {
			t.Fatal("描述契约时执行了业务调用")
			return "", nil
		},
		func(string) *adapterOutput { t.Fatal("描述契约时执行了响应组装"); return nil },
	)
	empty := NoContentEndpoint(NewOperation(Public, huma.Operation{OperationID: "delete-item", Method: http.MethodDelete, Path: "/item"}), func(_ struct{}, _ context.Context, _ *struct{}) error {
		t.Fatal("描述契约时执行了删除")
		return nil
	})
	mapped.Describe(api)
	empty.Describe(api)
	if api.OpenAPI().Paths["/mapped"].Get == nil || api.OpenAPI().Paths["/item"].Delete == nil {
		t.Fatal("离线契约缺少路由")
	}
}

func TestRoutePolicyIsExplicitAndControlsContract(t *testing.T) {
	for _, policy := range []AuthPolicy{Public, Session} {
		api := humachi.New(chi.NewRouter(), huma.DefaultConfig("test", "1"))
		route := Endpoint(NewOperation(policy, huma.Operation{OperationID: "policy", Method: http.MethodGet, Path: "/policy"}), func(struct{}, context.Context, *struct{}) (*struct{}, error) { return nil, nil })
		route.Describe(api)
		security := api.OpenAPI().Paths["/policy"].Get.Security
		if route.Policy() != policy {
			t.Fatal("运行时策略发生漂移")
		}
		if policy == Public && len(security) != 0 {
			t.Fatal("公开路由错误声明认证")
		}
		if policy == Session {
			if len(security) != 1 {
				t.Fatal("会话路由缺少认证声明")
			}
			if _, ok := security[0]["bearer"]; !ok {
				t.Fatal("会话路由缺少 Bearer 声明")
			}
		}
	}
	for _, tc := range []struct {
		name  string
		build func() Operation
	}{
		{"未声明", func() Operation { return Operation{} }},
		{"未知策略", func() Operation { return NewOperation("unknown", huma.Operation{}) }},
		{"独立声明 Security", func() Operation {
			return NewOperation(Public, huma.Operation{Security: []map[string][]string{{"bearer": {}}}})
		}},
		{"构造后修改 Security", func() Operation {
			op := NewOperation(Public, huma.Operation{})
			op.Security = []map[string][]string{{"bearer": {}}}
			return op
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("无效策略没有被拒绝")
				}
			}()
			Endpoint(tc.build(), func(struct{}, context.Context, *struct{}) (*struct{}, error) { return nil, nil })
		})
	}
}
