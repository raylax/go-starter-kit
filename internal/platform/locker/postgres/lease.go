package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/example/go-starter-kit/internal/platform/locker"
	"github.com/jackc/pgx/v5/pgxpool"
)

type lease struct {
	conn    *pgxpool.Conn
	key     lockKey
	options Options
}

// Watch 独占连接访问；Runner 等待其退出后才开始 Release。
func (l *lease) Watch(ctx context.Context) error {
	ticker := time.NewTicker(l.options.CheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// 正常停止监控不打断正在进行的探测，以免 pgx 关闭健康连接。
			probe, cancel := context.WithTimeout(context.WithoutCancel(ctx), l.options.OperationTimeout)
			err := l.conn.Conn().Ping(probe)
			cancel()
			if err != nil {
				return locker.Wrap(locker.ErrLockLost, err)
			}
		}
	}
}

func (l *lease) Release(ctx context.Context) error {
	if l.conn == nil {
		return nil
	}
	conn := l.conn
	l.conn = nil
	var unlocked bool
	err := conn.QueryRow(ctx, "SELECT pg_advisory_unlock($1::integer, $2::integer)", l.key.namespace, l.key.resource).Scan(&unlocked)
	if err == nil && unlocked {
		conn.Release()
		return nil
	}
	// 错误或锁已经消失时，销毁连接，避免污染连接池。
	closeErr := conn.Hijack().Close(ctx)
	if err == nil {
		err = locker.ErrLockLost
	}
	return locker.Wrap(locker.ErrRelease, errors.Join(err, closeErr))
}
