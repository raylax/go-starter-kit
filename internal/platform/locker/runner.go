package locker

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

type Options struct {
	Namespace      string
	AcquireTimeout time.Duration
	ReleaseTimeout time.Duration
	RetryMin       time.Duration
	RetryMax       time.Duration
}

type Runner struct {
	backend Backend
	options Options
	metrics *metrics
}

var _ Locker = (*Runner)(nil)

func New(backend Backend, o Options) (*Runner, error) {
	if backend == nil || !validKey(o.Namespace, 64) || strings.Contains(o.Namespace, ":") {
		return nil, fmt.Errorf("锁后端或命名空间无效")
	}
	if o.AcquireTimeout == 0 {
		o.AcquireTimeout = 5 * time.Second
	}
	if o.ReleaseTimeout == 0 {
		o.ReleaseTimeout = 5 * time.Second
	}
	if o.RetryMin == 0 {
		o.RetryMin = 50 * time.Millisecond
	}
	if o.RetryMax == 0 {
		o.RetryMax = time.Second
	}
	if o.AcquireTimeout < 0 || o.ReleaseTimeout < 0 || o.RetryMin < time.Millisecond || o.RetryMax < o.RetryMin {
		return nil, fmt.Errorf("锁超时或重试配置无效")
	}
	m, err := newMetrics()
	if err != nil {
		return nil, err
	}
	return &Runner{backend: backend, options: o, metrics: m}, nil
}

func validKey(key string, max int) bool {
	if len(key) == 0 || len(key) > max {
		return false
	}
	for _, c := range key {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == ':' || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

type heldKey struct {
	name   string
	parent *heldKey
}
type heldContextKey struct{}

func (r *Runner) prepare(ctx context.Context, key string, task Task) (string, error) {
	if !validKey(key, 256) {
		return "", ErrInvalidKey
	}
	if task == nil {
		return "", ErrInvalidTask
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	key = r.options.Namespace + ":" + key
	held, _ := ctx.Value(heldContextKey{}).(*heldKey)
	for h := held; h != nil; h = h.parent {
		if h.name == key {
			return "", ErrReentrant
		}
	}
	return key, nil
}

func (r *Runner) acquire(ctx context.Context, key string) (Lease, error) {
	attempt, cancel := context.WithTimeout(ctx, r.options.AcquireTimeout)
	defer cancel()
	start := time.Now()
	lease, err := r.backend.TryAcquire(attempt, key)
	outcome := "acquired"
	if err != nil {
		outcome = "error"
	} else if lease == nil {
		outcome = "busy"
	}
	r.metrics.acquisition(ctx, outcome, time.Since(start))
	if err != nil {
		return nil, Wrap(ErrUnavailable, err)
	}
	return lease, nil
}

func (r *Runner) TryRun(ctx context.Context, key string, task Task) (bool, error) {
	key, err := r.prepare(ctx, key, task)
	if err != nil {
		return false, err
	}
	lease, err := r.acquire(ctx, key)
	if err != nil || lease == nil {
		return false, err
	}
	return r.execute(ctx, key, lease, task)
}

func (r *Runner) Run(ctx context.Context, key string, task Task) error {
	key, err := r.prepare(ctx, key, task)
	if err != nil {
		return err
	}
	start := time.Now()
	waiting := true
	defer func() {
		if waiting {
			r.metrics.wait.Record(context.WithoutCancel(ctx), time.Since(start).Seconds())
		}
	}()
	delay := r.options.RetryMin
	for {
		lease, err := r.acquire(ctx, key)
		if err != nil {
			return err
		}
		if lease != nil {
			// 等待指标仅记录获取阶段，不包含回调时长。
			r.metrics.wait.Record(context.WithoutCancel(ctx), time.Since(start).Seconds())
			waiting = false
			_, err = r.execute(ctx, key, lease, task)
			return err
		}
		timer := time.NewTimer(delay/2 + time.Duration(rand.Int64N(int64(delay/2)+1)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if delay <= r.options.RetryMax/2 {
			delay *= 2
		} else {
			delay = r.options.RetryMax
		}
	}
}

func (r *Runner) execute(ctx context.Context, key string, lease Lease, task Task) (ran bool, err error) {
	work, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	watch, stop := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan error, 1)
	go func() {
		e := lease.Watch(watch)
		if watch.Err() != nil && errors.Is(e, watch.Err()) && !errors.Is(e, ErrLockLost) {
			done <- nil
			return
		}
		e = Wrap(ErrLockLost, e)
		cancel(e)
		done <- e
	}()
	start := time.Now()
	defer func() {
		if ran {
			r.metrics.execution.Record(context.WithoutCancel(ctx), time.Since(start).Seconds())
		}
		stop()
		lost := <-done
		if lost != nil {
			r.metrics.lost.Add(context.WithoutCancel(ctx), 1)
		}
		err = errors.Join(err, lost, context.Cause(work))
		releaseCtx, releaseCancel := context.WithTimeout(context.WithoutCancel(ctx), r.options.ReleaseTimeout)
		defer releaseCancel()
		if e := lease.Release(releaseCtx); e != nil {
			r.metrics.releaseErrors.Add(releaseCtx, 1)
			err = errors.Join(err, Wrap(ErrRelease, e))
		}
	}()
	if cause := context.Cause(work); cause != nil {
		return false, cause
	}
	parent, _ := ctx.Value(heldContextKey{}).(*heldKey)
	work = context.WithValue(work, heldContextKey{}, &heldKey{name: key, parent: parent})
	ran = true
	err = task(work)
	return ran, err
}
