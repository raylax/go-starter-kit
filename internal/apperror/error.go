// Package apperror 定义与传输协议无关的业务错误。
package apperror

import "fmt"

type Kind uint8

const (
	NotFound Kind = iota + 1
	Conflict
	Invalid
	Unauthenticated
	Unavailable
	Forbidden
	RateLimited
)

// Error 只保存可以向调用方公开的分类和提示，字段创建后不可修改。
type Error struct {
	kind    Kind
	message string
}

func New(kind Kind, message string) *Error { return &Error{kind: kind, message: message} }
func (e *Error) Error() string             { return e.message }
func (e *Error) Kind() Kind                { return e.kind }

var ErrUnauthenticated = New(Unauthenticated, "authenticated subject required")

// Wrap 同时保留具体业务错误的身份和底层原因；HTTP 层只公开 Error 的提示。
func Wrap(business *Error, cause error) error {
	if business == nil {
		return cause
	}
	if cause == nil {
		return business
	}
	return fmt.Errorf("%w: %w", business, cause)
}
