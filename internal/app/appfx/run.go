package appfx

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
)

// 为服务排空后的数据库和遥测关闭预留时间。
const infrastructureShutdownGrace = 10 * time.Second

// Failures 将后台服务的异常退出交给应用入口处理。
type Failures struct{ events chan error }

func (f *Failures) Report(err error) {
	if err == nil {
		return
	}
	select {
	case f.events <- err:
	default:
	}
}

// Run 使用命令入口的信号上下文，避免重复注册进程信号。
func Run(ctx context.Context, cfg Config, options ...fx.Option) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	failures := &Failures{events: make(chan error, 1)}
	startup := &startupContext{}
	options = append([]fx.Option{fx.Supply(cfg, failures), Module,
		fx.Decorate(func(lc fx.Lifecycle) fx.Lifecycle { return &startupLifecycle{Lifecycle: lc, startup: startup} }),
		// 根日志器由全部子模块继承，覆盖装配和生命周期事件。
		fx.WithLogger(func(logger *slog.Logger) fxevent.Logger {
			events := &fxevent.SlogLogger{Logger: logger}
			events.UseLogLevel(slog.LevelDebug)
			return events
		}), fx.StopTimeout(cfg.ShutdownTimeout + infrastructureShutdownGrace)}, options...)
	app := fx.New(options...)
	if err := app.Err(); err != nil {
		return err
	}
	return runApplication(ctx, app, startup, failures)
}

func runApplication(ctx context.Context, app *fx.App, startup *startupContext, failures *Failures) error {
	startupCtx, cancelStartup := context.WithTimeout(ctx, app.StartTimeout())
	defer cancelStartup()
	startup.context = startupCtx
	// Fx 自动回滚沿用 Start 的上下文，因此为其保留独立的停止预算。
	lifecycleCtx, cancelLifecycle := context.WithTimeout(context.WithoutCancel(ctx), app.StartTimeout()+app.StopTimeout())
	err := app.Start(lifecycleCtx)
	cancelLifecycle()
	// 最后一个启动钩子可能恰好在取消后成功，仍需关闭其资源并报告取消。
	if err == nil {
		err = startupCtx.Err()
	}
	cancelStartup()
	if err == nil {
		select {
		case <-ctx.Done():
		case err = <-failures.events:
		}
	}
	shutdown, stop := context.WithTimeout(context.Background(), app.StopTimeout())
	defer stop()
	return errors.Join(err, app.Stop(shutdown))
}
