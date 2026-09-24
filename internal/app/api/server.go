package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"go.uber.org/fx"
)

type httpService struct{}

func newHTTPService(in httpDependencies) *httpService {
	var server *http.Server
	var cancelLifetime context.CancelFunc
	var draining atomic.Bool
	in.Lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// 在基础设施启动后构造 handler，让遥测中间件使用已安装的 provider。
			handler, _, err := NewHandler(in.Config, in.Logger, Dependencies{
				Projects: in.Services.Projects, Tasks: in.Services.Tasks, Accounts: in.Services.Accounts, Authenticator: in.Services.Authenticator,
				Ready: func(ctx context.Context) error {
					if draining.Load() {
						return fmt.Errorf("服务正在停止")
					}
					return in.Database.Ping(ctx)
				},
			})
			if err != nil {
				return err
			}
			listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", in.Config.HTTPAddr)
			if err != nil {
				return fmt.Errorf("监听 HTTP: %w", err)
			}
			// 停止信号不直接取消在途请求，由 OnStop 先排空。
			lifetime, cancel := context.WithCancel(context.Background())
			cancelLifetime = cancel
			server = &http.Server{
				Addr: in.Config.HTTPAddr, Handler: handler, ReadHeaderTimeout: 5 * time.Second,
				ReadTimeout: 10 * time.Second, WriteTimeout: in.Config.RequestTimeout + 5*time.Second,
				IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10,
				BaseContext: func(net.Listener) context.Context { return lifetime },
				ErrorLog:    slog.NewLogLogger(in.Logger.Handler(), slog.LevelError),
			}
			go func() {
				if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
					in.Failures.Report(err)
				}
			}()
			in.Logger.Info("api listening", "address", listener.Addr().String(), "environment", in.Config.Environment)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			drain, cancel := context.WithTimeout(ctx, in.Config.ShutdownTimeout)
			defer cancel()
			draining.Store(true)
			in.Logger.Info("draining requests")
			defer cancelLifetime()
			if err := server.Shutdown(drain); err != nil {
				cancelLifetime()
				_ = server.Close()
				return fmt.Errorf("排空 HTTP 请求: %w", err)
			}
			return nil
		},
	})
	return &httpService{}
}
