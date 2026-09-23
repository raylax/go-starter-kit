package locker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type fakeBackend struct {
	acquire func(context.Context, string) (Lease, error)
}

func (b fakeBackend) TryAcquire(ctx context.Context, key string) (Lease, error) {
	return b.acquire(ctx, key)
}

type fakeLease struct {
	lost             chan struct{}
	releaseErr       error
	releases         atomic.Int32
	releasedCanceled atomic.Bool
}

func (l *fakeLease) Watch(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.lost:
		return ErrLockLost
	}
}
func (l *fakeLease) Release(ctx context.Context) error {
	l.releases.Add(1)
	l.releasedCanceled.Store(ctx.Err() != nil)
	return l.releaseErr
}
func runnerFor(t *testing.T, b Backend) *Runner {
	t.Helper()
	r, err := New(b, Options{Namespace: "test", RetryMin: time.Millisecond, RetryMax: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestBusyAndWaitCancellation(t *testing.T) {
	var calls int
	r := runnerFor(t, fakeBackend{func(context.Context, string) (Lease, error) { return nil, nil }})
	task := func(context.Context) error { calls++; return nil }
	if ran, err := r.TryRun(t.Context(), "busy", task); ran || err != nil || calls != 0 {
		t.Fatalf("竞争结果: %v %v", ran, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := r.Run(ctx, "busy", task); !errors.Is(err, context.DeadlineExceeded) || calls != 0 {
		t.Fatalf("等待未取消: %v", err)
	}
}
func TestLostCancelsTaskAndPreservesErrors(t *testing.T) {
	l := &fakeLease{lost: make(chan struct{}), releaseErr: errors.New("release details")}
	r := runnerFor(t, fakeBackend{func(context.Context, string) (Lease, error) { return l, nil }})
	taskErr := errors.New("task failed")
	ran, err := r.TryRun(t.Context(), "lost", func(ctx context.Context) error {
		close(l.lost)
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
			t.Fatal("丢锁未取消任务")
		}
		if !errors.Is(context.Cause(ctx), ErrLockLost) {
			t.Error("取消原因丢失")
		}
		return taskErr
	})
	if !ran || !errors.Is(err, taskErr) || !errors.Is(err, ErrLockLost) || !errors.Is(err, ErrRelease) || !errors.Is(err, l.releaseErr) {
		t.Fatalf("错误链缺失: %v", err)
	}
	if l.releases.Load() != 1 || l.releasedCanceled.Load() {
		t.Fatal("释放次数或清理上下文错误")
	}
}
func TestPanicAndReentry(t *testing.T) {
	l := &fakeLease{}
	r := runnerFor(t, fakeBackend{func(context.Context, string) (Lease, error) { return l, nil }})
	func() {
		defer func() {
			if recover() != "task panic" {
				t.Error("panic 未传播")
			}
		}()
		_, _ = r.TryRun(t.Context(), "same", func(ctx context.Context) error {
			if err := r.Run(ctx, "same", func(context.Context) error { return nil }); !errors.Is(err, ErrReentrant) {
				t.Errorf("未拒绝重入: %v", err)
			}
			panic("task panic")
		})
	}()
	if l.releases.Load() != 1 {
		t.Fatal("panic 后未释放")
	}
}
func TestCancellationAfterAcquireDoesNotRun(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	l := &fakeLease{}
	r := runnerFor(t, fakeBackend{func(context.Context, string) (Lease, error) { cancel(); return l, nil }})
	ran, err := r.TryRun(ctx, "canceled", func(context.Context) error { t.Fatal("取消后启动了任务"); return nil })
	if ran || !errors.Is(err, context.Canceled) || l.releases.Load() != 1 || l.releasedCanceled.Load() {
		t.Fatalf("取消结果错误: %v %v", ran, err)
	}
}
