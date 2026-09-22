package httpapi

import "github.com/example/go-starter-kit/internal/pagination"

// PageQuery 集中声明查询参数标签；业务层使用不含 HTTP 标签的 Params。
type PageQuery struct {
	Limit  int32 `query:"limit" default:"20" minimum:"1" maximum:"100"`
	Offset int32 `query:"offset" default:"0" minimum:"0" maximum:"10000"`
}

func (p PageQuery) Params() pagination.Params {
	return pagination.Params{Limit: p.Limit, Offset: p.Offset}
}

// pageFields 集中维护分页字段，同时支持具名 DTO 的编译期约束。
type pageFields[T any] = struct {
	Items   []T   `json:"items"`
	HasMore bool  `json:"has_more"`
	Limit   int32 `json:"limit"`
	Offset  int32 `json:"offset"`
}

// Page 是公共分页正文，模块可以定义具名类型来固定 schema 名称。
type Page[T any] pageFields[T]

func PageFrom[S, T any](result pagination.Result[S], convert func(S) T) Page[T] {
	mapped := pagination.Map(result, convert)
	return Page[T]{Items: mapped.Items, HasMore: mapped.HasMore, Limit: mapped.Limit, Offset: mapped.Offset}
}
