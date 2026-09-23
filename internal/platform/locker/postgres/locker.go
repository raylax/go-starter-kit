// Package postgres 使用专用连接池实现 PostgreSQL session advisory lock。
package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/example/go-starter-kit/internal/platform/locker"
	"github.com/jackc/pgx/v5/pgxpool"
)

// lockKey 使用 PostgreSQL 的双 int32 锁空间，与 bigint 锁空间不同。
type lockKey struct{ namespace, resource int32 }

type Options struct {
	CheckInterval    time.Duration
	OperationTimeout time.Duration
}

type Backend struct {
	pool    *pgxpool.Pool
	options Options
}

var _ locker.Backend = (*Backend)(nil)

// New 不接管连接池的关闭；调用方必须提供直连或 session pooling 的专用池。
func New(pool *pgxpool.Pool, o Options) (*Backend, error) {
	if pool == nil {
		return nil, fmt.Errorf("锁连接池不能为空")
	}
	if o.CheckInterval == 0 {
		o.CheckInterval = 2 * time.Second
	}
	if o.OperationTimeout == 0 {
		o.OperationTimeout = 2 * time.Second
	}
	if o.CheckInterval < 0 || o.OperationTimeout < 0 {
		return nil, fmt.Errorf("锁检测配置无效")
	}
	return &Backend{pool: pool, options: o}, nil
}

func keyFor(name string) lockKey {
	hash := sha256.Sum256([]byte(name))
	return lockKey{namespace: int32(binary.BigEndian.Uint32(hash[:4])), resource: int32(binary.BigEndian.Uint32(hash[4:8]))}
}

func (b *Backend) TryAcquire(ctx context.Context, name string) (locker.Lease, error) {
	conn, err := b.pool.Acquire(ctx)
	if err != nil {
		return nil, locker.Wrap(locker.ErrUnavailable, err)
	}
	key := keyFor(name)
	var acquired bool
	err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1::integer, $2::integer)", key.namespace, key.resource).Scan(&acquired)
	if err != nil {
		// 服务端可能已经加锁，但响应未到达；绝不能归还这条连接。
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), b.options.OperationTimeout)
		defer cancel()
		closeErr := conn.Hijack().Close(cleanup)
		return nil, locker.Wrap(locker.ErrUnavailable, errors.Join(err, closeErr))
	}
	if !acquired {
		conn.Release()
		return nil, nil
	}
	return &lease{conn: conn, key: key, options: b.options}, nil
}
