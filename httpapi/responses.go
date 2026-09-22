package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/example/go-starter-kit/pagination"
)

// ItemOutput 包装 DTO 正文，不持有业务模型或数据库行。
type ItemOutput[D any] struct{ Body D }

type CreatedOutput[D any] struct {
	Location string `header:"Location"`
	Body     D
}

// CreatedEndpoint 在业务成功后转换 DTO，再从 DTO 生成 Location。
func CreatedEndpoint[S, I, T, D any](operation huma.Operation, execute func(S, context.Context, *I) (T, error), convert func(T) D, location func(D) string) Route[S] {
	operation.DefaultStatus = http.StatusCreated
	return MapEndpoint(operation, execute, func(value T) *CreatedOutput[D] {
		dto := convert(value)
		return &CreatedOutput[D]{Location: location(dto), Body: dto}
	})
}

// ItemEndpoint 统一单条资源的 DTO 转换和 200 响应包装。
func ItemEndpoint[S, I, T, D any](operation huma.Operation, execute func(S, context.Context, *I) (T, error), convert func(T) D) Route[S] {
	operation.DefaultStatus = http.StatusOK
	return MapEndpoint(operation, execute, func(value T) *ItemOutput[D] {
		return &ItemOutput[D]{Body: convert(value)}
	})
}

// PageEndpoint 的首个类型参数是模块的具名分页 DTO，用于保持 OpenAPI 名称。
// B 的结构在编译期必须与 Page[D] 一致，运行时不使用反射转换。
func PageEndpoint[B interface{ ~pageFields[D] }, S, I, T, D any](operation huma.Operation, execute func(S, context.Context, *I) (pagination.Result[T], error), convert func(T) D) Route[S] {
	operation.DefaultStatus = http.StatusOK
	return MapEndpoint(operation, execute, func(page pagination.Result[T]) *ItemOutput[B] {
		return &ItemOutput[B]{Body: B(PageFrom(page, convert))}
	})
}
