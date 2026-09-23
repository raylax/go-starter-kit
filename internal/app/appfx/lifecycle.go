package appfx

import (
	"context"

	"go.uber.org/fx"
)

// startupContext 在构建依赖图后、执行启动钩子前设置。
// 业务启动遵循该上下文，Fx 自身持有额外的回滚时间。
type startupContext struct{ context context.Context }

type startupLifecycle struct {
	fx.Lifecycle
	startup *startupContext
}

func (l *startupLifecycle) Append(hook fx.Hook) {
	if start := hook.OnStart; start != nil {
		hook.OnStart = func(context.Context) error {
			if err := l.startup.context.Err(); err != nil {
				return err
			}
			return start(l.startup.context)
		}
	}
	l.Lifecycle.Append(hook)
}
