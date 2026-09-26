package appfx

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Database 仅向业务服务提供查询、事务和健康检查能力。
// 连接池由生命周期钩子打开和关闭，只能由更晚启动的服务使用。
type Database struct {
	pool *pgxpool.Pool
}

func (d *Database) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return d.pool.Exec(ctx, sql, args...)
}

func (d *Database) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return d.pool.Query(ctx, sql, args...)
}

func (d *Database) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return d.pool.QueryRow(ctx, sql, args...)
}

func (d *Database) Begin(ctx context.Context) (pgx.Tx, error) {
	return d.pool.Begin(ctx)
}

func (d *Database) Ping(ctx context.Context) error {
	return d.pool.Ping(ctx)
}
