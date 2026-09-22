package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/example/go-starter-kit/internal/apperror"
	"github.com/example/go-starter-kit/internal/httpapi"
	"github.com/example/go-starter-kit/internal/pagination"
)

// 使用不同字段形状验证适配器只公开 DTO，而不是直接序列化业务模型。
type responseRecord struct {
	Key    string
	Secret string
}

type responseDTO struct {
	ID string `json:"id"`
}

type responsePage httpapi.Page[responseDTO]

func TestResourceResponseAdapters(t *testing.T) {
	for _, created := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			t.Run(map[bool]string{false: "单条", true: "创建"}[created]+map[bool]string{false: "成功", true: "失败"}[fail], func(t *testing.T) {
				calls, conversions, locations := 0, 0, 0
				execute := func(struct{}, context.Context, *struct{}) (responseRecord, error) {
					calls++
					value := responseRecord{Key: "raw", Secret: "private-value"}
					if fail {
						return value, apperror.New(apperror.Conflict, "already exists")
					}
					return value, nil
				}
				convert := func(value responseRecord) responseDTO {
					conversions++
					return responseDTO{ID: "dto-" + value.Key}
				}
				op := huma.Operation{OperationID: "resource", Method: http.MethodPost, Path: "/resource"}
				route := httpapi.ItemEndpoint(op, execute, convert)
				if created {
					route = httpapi.CreatedEndpoint(op, execute, convert, func(dto responseDTO) string {
						locations++
						return "/resource/" + dto.ID
					})
				}
				router := chi.NewRouter()
				api := humachi.New(router, huma.DefaultConfig("响应测试", "1"))
				route.Describe(humachi.New(chi.NewRouter(), huma.DefaultConfig("离线契约", "1")))
				if calls+conversions+locations != 0 {
					t.Fatal("离线描述执行了回调")
				}
				route.Bind(api, struct{}{})
				res := httptest.NewRecorder()
				router.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/resource", nil))
				if calls != 1 || strings.Contains(res.Body.String(), "private-value") {
					t.Fatal("业务调用次数异常或业务私有字段泄露")
				}
				if fail {
					if res.Code != http.StatusConflict || conversions+locations != 0 || res.Header().Get("Location") != "" {
						t.Fatal("错误路径转换了 DTO 或未统一映射错误")
					}
					return
				}
				status, locationCalls, location := http.StatusOK, 0, ""
				if created {
					status, locationCalls, location = http.StatusCreated, 1, "/resource/dto-raw"
				}
				var body responseDTO
				if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || body.ID != "dto-raw" || conversions != 1 || locations != locationCalls || res.Code != status || res.Header().Get("Location") != location {
					t.Fatalf("响应正文、状态或 Location 异常：%d %s", res.Code, res.Body.String())
				}
			})
		}
	}
}

func TestPageResponseAdapter(t *testing.T) {
	for _, tc := range []struct {
		name  string
		items []responseRecord
		fail  bool
	}{
		{name: "有序条目", items: []responseRecord{{Key: "b", Secret: "private-value"}, {Key: "a"}}},
		{name: "空列表"},
		{name: "错误忽略返回值", items: []responseRecord{{Key: "ignored"}}, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, conversions := 0, 0
			route := httpapi.PageEndpoint[responsePage](huma.Operation{OperationID: "page", Method: http.MethodGet, Path: "/page"},
				func(struct{}, context.Context, *struct{}) (pagination.Result[responseRecord], error) {
					calls++
					page := pagination.Result[responseRecord]{Items: tc.items, HasMore: len(tc.items) > 0, Limit: 2, Offset: 4}
					if tc.fail {
						return page, apperror.New(apperror.NotFound, "not found")
					}
					return page, nil
				}, func(value responseRecord) responseDTO {
					conversions++
					return responseDTO{ID: value.Key}
				})
			router := chi.NewRouter()
			api := humachi.New(router, huma.DefaultConfig("分页测试", "1"))
			description := humachi.New(chi.NewRouter(), huma.DefaultConfig("分页测试", "1"))
			route.Describe(description)
			if calls+conversions != 0 {
				t.Fatal("离线描述执行了回调")
			}
			route.Bind(api, struct{}{})
			offline, err := json.Marshal(description.OpenAPI())
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := json.Marshal(api.OpenAPI())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(offline, runtime) {
				t.Fatal("离线契约与运行时契约不一致")
			}
			schema := api.OpenAPI().Paths["/page"].Get.Responses["200"].Content["application/json"].Schema
			if schema.Ref != "#/components/schemas/ResponsePage" {
				t.Fatalf("具名分页 schema 丢失：%s", schema.Ref)
			}
			res := httptest.NewRecorder()
			router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/page", nil))
			if calls != 1 || strings.Contains(res.Body.String(), "private-value") {
				t.Fatal("业务调用次数异常或业务私有字段泄露")
			}
			if tc.fail {
				if res.Code != http.StatusNotFound || conversions != 0 {
					t.Fatal("错误路径转换了条目或未统一映射错误")
				}
				return
			}
			var page responsePage
			if err := json.Unmarshal(res.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if res.Code != http.StatusOK || page.Items == nil || len(page.Items) != len(tc.items) || conversions != len(tc.items) || page.Limit != 2 || page.Offset != 4 || page.HasMore != (len(tc.items) > 0) {
				t.Fatalf("分页正文或转换次数异常：%s", res.Body.String())
			}
			for i, item := range page.Items {
				if item.ID != tc.items[i].Key {
					t.Fatal("条目顺序变化")
				}
			}
		})
	}
}
