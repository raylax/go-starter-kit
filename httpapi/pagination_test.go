package httpapi

import (
	"encoding/json"
	"reflect"
	"strconv"
	"testing"

	"github.com/example/go-starter-kit/pagination"
)

func TestPaginationContractMatchesPolicy(t *testing.T) {
	typ := reflect.TypeFor[PageQuery]()
	limit, _ := typ.FieldByName("Limit")
	offset, _ := typ.FieldByName("Offset")
	for _, tc := range []struct {
		got  string
		want int32
	}{
		{limit.Tag.Get("default"), pagination.DefaultLimit}, {limit.Tag.Get("minimum"), 1}, {limit.Tag.Get("maximum"), pagination.MaxLimit},
		{offset.Tag.Get("default"), 0}, {offset.Tag.Get("minimum"), 0}, {offset.Tag.Get("maximum"), pagination.MaxOffset},
	} {
		if tc.got != strconv.FormatInt(int64(tc.want), 10) {
			t.Fatal("分页契约与运行时政策不一致")
		}
	}
	page := PageFrom(pagination.Result[int]{Limit: 20}, func(i int) string { return strconv.Itoa(i) })
	data, err := json.Marshal(page)
	if err != nil || string(data) != `{"items":[],"has_more":false,"limit":20,"offset":0}` {
		t.Fatalf("空页结构发生变化：%s", data)
	}
}
