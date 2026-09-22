//go:build integration

package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	pgcontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/example/go-starter-kit/db"
)

// Database 启动真实 PostgreSQL 并应用迁移；Docker 不可用时直接失败。
func Database(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	ctx := t.Context()
	container, err := pgcontainer.Run(ctx, "postgres:18-alpine",
		pgcontainer.WithDatabase("starter"), pgcontainer.WithUsername("starter"), pgcontainer.WithPassword("integration-only"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(time.Minute)),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := container.Terminate(cleanupCtx); err != nil {
			t.Errorf("terminate database: %v", err)
		}
	})
	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, databaseURL, "up"); err != nil {
		t.Fatal(err)
	}
	// 重复应用当前迁移集合必须安全。
	if err := db.Migrate(ctx, databaseURL, "up"); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Open(ctx, databaseURL, 5)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, databaseURL
}
