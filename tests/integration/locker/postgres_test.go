//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/platform/locker"
	"github.com/example/go-starter-kit/internal/platform/locker/postgres"
	"github.com/example/go-starter-kit/tests/integration/testutil"
)

func TestPostgresLockLifecycle(t *testing.T) {
	_, databaseURL := testutil.Database(t)
	pool, err := db.Open(t.Context(), databaseURL, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	backend, err := postgres.New(pool, postgres.Options{CheckInterval: time.Millisecond, OperationTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := locker.New(backend, locker.Options{Namespace: "test"})
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("rollback")
	ran, err := runner.TryRun(t.Context(), "cleanup", func(context.Context) error {
		// 使用独立 context 模拟其他进程；派生 context 中的重入由公共层拒绝。
		otherRan, e := runner.TryRun(t.Context(), "cleanup", func(context.Context) error { t.Error("重复执行"); return nil })
		if otherRan || e != nil {
			t.Errorf("竞争结果: %v %v", otherRan, e)
		}
		return failure
	})
	if !ran || !errors.Is(err, failure) {
		t.Fatalf("回调结果: %v %v", ran, err)
	}
	if ran, err := runner.TryRun(t.Context(), "cleanup", func(context.Context) error { return nil }); !ran || err != nil {
		t.Fatalf("错误后未释放: %v %v", ran, err)
	}
}
