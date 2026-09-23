// Package locker 协调同一命名空间内的后台任务，不提供 exactly-once 或 fencing 保证。
package locker

import (
	"context"
	"errors"
)

type Task func(context.Context) error

// Locker 的回调必须遵循传入的 context；同名锁不支持重入。
// TryRun 的 ran 只表示回调已经进入，不表示业务提交成功。
type Locker interface {
	TryRun(context.Context, string, Task) (ran bool, err error)
	Run(context.Context, string, Task) error
}

var (
	ErrInvalidKey  = errors.New("锁名称无效")
	ErrInvalidTask = errors.New("锁回调不能为空")
	ErrReentrant   = errors.New("不支持锁重入")
	ErrUnavailable = errors.New("锁服务不可用")
	ErrLockLost    = errors.New("锁持有权丢失或无法确认")
	ErrRelease     = errors.New("锁释放失败")
)

// failure 保留错误链供分类使用，但公开文本不包含后端连接信息。
type failure struct{ kind, cause error }

func (e *failure) Error() string   { return e.kind.Error() }
func (e *failure) Unwrap() []error { return []error{e.kind, e.cause} }
func Wrap(kind, cause error) error {
	if cause == nil {
		return kind
	}
	return &failure{kind: kind, cause: cause}
}
