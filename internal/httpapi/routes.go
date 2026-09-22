package httpapi

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
)

// Route 共享接口契约与处理函数的类型，分别支持运行时绑定和离线描述。
type Route[S any] interface {
	Bind(huma.API, S, ...func(huma.Context, func(huma.Context)))
	Describe(huma.API)
}

type endpoint[S, I, O any] struct {
	operation huma.Operation
	handle    func(S, context.Context, *I) (*O, error)
}

func Endpoint[S, I, O any](operation huma.Operation, handle func(S, context.Context, *I) (*O, error)) Route[S] {
	return endpoint[S, I, O]{operation: operation, handle: handle}
}

// MapEndpoint 分离业务调用和响应组装；调用失败时不执行响应转换。
func MapEndpoint[S, I, T, O any](operation huma.Operation, execute func(S, context.Context, *I) (T, error), present func(T) *O) Route[S] {
	return Endpoint(operation, func(service S, ctx context.Context, input *I) (*O, error) {
		value, err := execute(service, ctx, input)
		if err != nil {
			return nil, err
		}
		return present(value), nil
	})
}

// NoContentEndpoint 适配只返回错误的业务调用，成功时无响应正文。
func NoContentEndpoint[S, I any](operation huma.Operation, execute func(S, context.Context, *I) error) Route[S] {
	return Endpoint(operation, func(service S, ctx context.Context, input *I) (*struct{}, error) {
		return nil, execute(service, ctx, input)
	})
}

func (e endpoint[S, I, O]) Bind(api huma.API, service S, middlewares ...func(huma.Context, func(huma.Context))) {
	operation := e.operation
	operation.Middlewares = append(append(huma.Middlewares{}, operation.Middlewares...), middlewares...)
	huma.Register(api, operation, func(ctx context.Context, input *I) (*O, error) {
		output, err := e.handle(service, ctx, input)
		if err != nil {
			return nil, FromError(ctx, err)
		}
		return output, nil
	})
}

func (e endpoint[S, I, O]) Describe(api huma.API) {
	// 离线路由器只用于类型推导，不暴露处理请求的入口，也不持有运行时依赖。
	huma.Register(api, e.operation, func(context.Context, *I) (*O, error) {
		return nil, huma.Error501NotImplemented("contract only")
	})
}
