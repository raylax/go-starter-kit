package worker

import (
	"context"
	"log/slog"

	"github.com/example/go-starter-kit/internal/app/appfx"
	"github.com/example/go-starter-kit/internal/modules/mailoutbox"
	"github.com/example/go-starter-kit/internal/platform/mail"
	"go.uber.org/fx"
)

var Module = fx.Module("worker",
	fx.Provide(
		func(logger *slog.Logger) mail.Sender { return mail.NewLogSender(logger) },
		func(database *appfx.Database, sender mail.Sender, logger *slog.Logger) (*mailoutbox.Service, error) {
			return mailoutbox.NewService(database, func(ctx context.Context, m mailoutbox.Message) error {
				return sender.Send(ctx, mail.Message{ID: m.ID, Kind: m.Kind, To: m.To, Subject: m.Subject, HTML: m.HTML})
			}, logger)
		},
	),
	fx.Invoke(startMailConsumer),
)

func startMailConsumer(lc fx.Lifecycle, cfg Config, service *mailoutbox.Service, logger *slog.Logger, failures *appfx.Failures) {
	var cancel context.CancelFunc
	var done chan struct{}
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			var run context.Context
			run, cancel = context.WithCancel(context.Background())
			done = make(chan struct{})
			go func() {
				defer close(done)
				failures.Report(service.Run(run, cfg.MailPollInterval))
			}()
			logger.Info("Worker 已启动")
			return nil
		},
		OnStop: func(ctx context.Context) error {
			ctx, stop := context.WithTimeout(ctx, cfg.ShutdownTimeout)
			defer stop()
			cancel()
			select {
			case <-done:
				logger.Info("Worker 已停止")
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
}
