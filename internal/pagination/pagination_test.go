package pagination_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/example/go-starter-kit/internal/pagination"
)

func TestParameterBoundaries(t *testing.T) {
	for _, p := range []pagination.Params{{}, {Limit: -1}, {Limit: 101}, {Limit: 1, Offset: -1}, {Limit: 1, Offset: 10001}, {Limit: 1<<31 - 1}} {
		if !errors.Is(p.Validate(), pagination.ErrInvalid) {
			t.Fatalf("错误参数被接受：%+v", p)
		}
		if _, err := pagination.Build([]int{1}, p, func(v int) int { return v }); !errors.Is(err, pagination.ErrInvalid) {
			t.Fatal("组装结果未拒绝非法参数")
		}
	}
	for _, p := range []pagination.Params{{Limit: 1}, {Limit: pagination.DefaultLimit}, {Limit: pagination.MaxLimit, Offset: pagination.MaxOffset}} {
		if p.Validate() != nil || p.FetchLimit() != p.Limit+1 {
			t.Fatalf("合法边界被拒绝：%+v", p)
		}
	}
}

func TestBuildAndMapPreservePage(t *testing.T) {
	for _, tc := range []struct {
		rows []int
		want []int
		more bool
	}{
		{nil, []int{}, false}, {[]int{1}, []int{10}, false}, {[]int{1, 2}, []int{10, 20}, false}, {[]int{1, 2, 3}, []int{10, 20}, true},
	} {
		calls := 0
		page, err := pagination.Build(tc.rows, pagination.Params{Limit: 2, Offset: 8}, func(v int) int { calls++; return v * 10 })
		if err != nil || !reflect.DeepEqual(page.Items, tc.want) || page.HasMore != tc.more || page.Limit != 2 || page.Offset != 8 || calls != len(tc.want) {
			t.Fatalf("分页或转换错误：%+v, %v", page, err)
		}
		mapped := pagination.Map(page, func(v int) int { return v + 1 })
		if mapped.HasMore != page.HasMore || mapped.Limit != page.Limit || mapped.Offset != page.Offset || mapped.Items == nil {
			t.Fatal("转换丢失元数据或空数组语义")
		}
	}
}
