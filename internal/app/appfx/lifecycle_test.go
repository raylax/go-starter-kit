package appfx

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"go.uber.org/fx"
)

func TestStartupFailureCleansResources(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout", "error"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			startup := &startupContext{}
			failures := &Failures{events: make(chan error, 1)}
			var stopped []string
			sentinel := errors.New("startup failed")
			app := fx.New(fx.NopLogger, fx.StartTimeout(50*time.Millisecond), fx.StopTimeout(time.Second),
				fx.Decorate(func(lc fx.Lifecycle) fx.Lifecycle { return &startupLifecycle{Lifecycle: lc, startup: startup} }),
				fx.Invoke(func(lc fx.Lifecycle) {
					for _, name := range []string{"telemetry", "database"} {
						lc.Append(fx.Hook{OnStart: func(context.Context) error { return nil }, OnStop: func(ctx context.Context) error {
							if ctx.Err() != nil {
								t.Error("清理上下文已取消")
							}
							stopped = append(stopped, name)
							return nil
						}})
					}
					lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
						if mode == "error" {
							return sentinel
						}
						if mode == "cancel" {
							cancel()
						}
						<-ctx.Done()
						return ctx.Err()
					}})
				}),
			)
			if err := app.Err(); err != nil {
				t.Fatal(err)
			}
			err := runApplication(ctx, app, startup, failures)
			want := sentinel
			if mode == "cancel" {
				want = context.Canceled
			}
			if mode == "timeout" {
				want = context.DeadlineExceeded
			}
			if !errors.Is(err, want) {
				t.Fatalf("启动错误未保留: %v", err)
			}
			if !reflect.DeepEqual(stopped, []string{"database", "telemetry"}) {
				t.Fatalf("资源未按逆序恰好清理一次: %v", stopped)
			}
		})
	}
}
