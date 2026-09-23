package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Beginner 是事务执行需要的最小数据库能力。
type Beginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

// WithTransaction 在回调成功后提交，否则回滚；panic 仍向上传播。
// 回滚使用独立且有界的上下文，避免请求取消阻止释放事务资源。
// 回调不应自行提交或回滚，也不应执行外部网络操作；数据库错误按调用方提供的策略映射，业务错误保持原有语义。
func WithTransaction(ctx context.Context, database Beginner, policy ErrorPolicy, fn func(pgx.Tx) error) error {
	tx, err := database.Begin(ctx)
	if err != nil {
		return MapError(err, policy)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if err := fn(tx); err != nil {
		return MapError(err, policy)
	}
	return MapError(tx.Commit(ctx), policy)
}
