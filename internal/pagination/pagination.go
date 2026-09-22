// Package pagination 统一分页参数、边界和结果转换，不依赖 HTTP 或数据库。
package pagination

import "errors"

const (
	DefaultLimit int32 = 20
	MaxLimit     int32 = 100
	MaxOffset    int32 = 10000
)

var ErrInvalid = errors.New("invalid pagination parameters")

type Params struct{ Limit, Offset int32 }

func (p Params) Validate() error {
	if p.Limit < 1 || p.Limit > MaxLimit || p.Offset < 0 || p.Offset > MaxOffset {
		return ErrInvalid
	}
	return nil
}

// FetchLimit 在参数校验成功后使用，多取一条记录用于判断下一页。
func (p Params) FetchLimit() int32 { return p.Limit + 1 }

type Result[T any] struct {
	Items   []T
	HasMore bool
	Limit   int32
	Offset  int32
}

// Build 接收按稳定顺序查询的记录，截断额外项并转换成业务类型。
func Build[S, T any](rows []S, params Params, convert func(S) T) (Result[T], error) {
	if err := params.Validate(); err != nil {
		return Result[T]{}, err
	}
	more := len(rows) > int(params.Limit)
	if more {
		rows = rows[:params.Limit]
	}
	return Map(Result[S]{Items: rows, HasMore: more, Limit: params.Limit, Offset: params.Offset}, convert), nil
}

// Map 保留分页元数据，空列表始终转换为非 nil 切片。
func Map[S, T any](result Result[S], convert func(S) T) Result[T] {
	items := make([]T, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, convert(item))
	}
	return Result[T]{Items: items, HasMore: result.HasMore, Limit: result.Limit, Offset: result.Offset}
}
