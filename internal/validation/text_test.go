package validation_test

import (
	"strings"
	"testing"

	"github.com/example/go-starter-kit/internal/validation"
)

func TestTextRules(t *testing.T) {
	for _, tc := range []struct {
		value string
		max   int
		valid bool
	}{
		{"", 0, true}, {"中文", 2, true}, {"中文", 1, false}, {"内容\x00", 10, false}, {" x ", 3, true}, {"", -1, false},
	} {
		if validation.TextWithin(tc.value, tc.max) != tc.valid {
			t.Fatalf("文本边界错误：%q", tc.value)
		}
	}
	for _, tc := range []struct {
		value string
		max   int
		want  string
		valid bool
	}{
		{" \t\n", 100, "", false}, {"　中文项目　", 4, "中文项目", true}, {" x\x00 ", 10, "x\x00", false}, {strings.Repeat("中", 101), 100, strings.Repeat("中", 101), false},
	} {
		value, valid := validation.RequiredText(tc.value, tc.max)
		if value != tc.want || valid != tc.valid {
			t.Fatalf("必填字段校验错误：%q", tc.value)
		}
	}
}
