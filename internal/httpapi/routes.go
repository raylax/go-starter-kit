package httpapi

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
)

// Route 共享接口契约与处理函数的类型，分别支持运行时绑定和离线描述。
type Route[S any] interface {
	Bind(huma.API, S, ...func(huma.Context, func(huma.Context)))
	Describe(huma.API)
	Policy() AuthPolicy
}

type AuthPolicy string

const (
	Public  AuthPolicy = "public"
	Session AuthPolicy = "bearer"
)

// Operation 以显式策略声明访问要求，OpenAPI Security 由策略单向生成。
type Operation struct {
	huma.Operation
	policy AuthPolicy
}

func NewOperation(policy AuthPolicy, op huma.Operation) Operation {
	operation := Operation{Operation: op, policy: policy}
	_ = operation.contract()
	return operation
}

func (o Operation) contract() huma.Operation {
	if o.policy != Public && o.policy != Session {
		panic("路由缺少认证策略")
	}
	if o.Security != nil {
		panic("Security 由认证策略生成，不能独立声明")
	}
	op := o.Operation
	if o.policy == Session {
		op.Security = []map[string][]string{{string(Session): {}}}
	}
	return op
}

func (e endpoint[S, I, O]) Policy() AuthPolicy { return e.operation.policy }

type endpoint[S, I, O any] struct {
	operation Operation
	handle    func(S, context.Context, *I) (*O, error)
}

func Endpoint[S, I, O any](operation Operation, handle func(S, context.Context, *I) (*O, error)) Route[S] {
	_ = operation.contract()
	return endpoint[S, I, O]{operation: operation, handle: handle}
}

// MapEndpoint 分离业务调用和响应组装；调用失败时不执行响应转换。
func MapEndpoint[S, I, T, O any](operation Operation, execute func(S, context.Context, *I) (T, error), present func(T) *O) Route[S] {
	return Endpoint(operation, func(service S, ctx context.Context, input *I) (*O, error) {
		value, err := execute(service, ctx, input)
		if err != nil {
			return nil, err
		}
		return present(value), nil
	})
}

// NoContentEndpoint 适配只返回错误的业务调用，成功时无响应正文。
func NoContentEndpoint[S, I any](operation Operation, execute func(S, context.Context, *I) error) Route[S] {
	return Endpoint(operation, func(service S, ctx context.Context, input *I) (*struct{}, error) {
		return nil, execute(service, ctx, input)
	})
}

func (e endpoint[S, I, O]) Bind(api huma.API, service S, middlewares ...func(huma.Context, func(huma.Context))) {
	operation := e.operation.contract()
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
	huma.Register(api, e.operation.contract(), func(context.Context, *I) (*O, error) {
		return nil, huma.Error501NotImplemented("contract only")
	})
}
