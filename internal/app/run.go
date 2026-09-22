package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/platform/telemetry"
)

// Run 加载配置、启动依赖和 HTTP 服务，并在停止信号到来时排空请求。
func Run(signalCtx context.Context) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)
	// 排空请求期间保持处理中的请求与 JWKS 刷新可用。
	lifetime, cancelLifetime := context.WithCancel(context.Background())
	defer cancelLifetime()
	// 启动时的停止信号必须取消同步依赖请求。
	// 启动完成后解除此回调，让处理中的 HTTP 请求正常结束。
	stopStartupCancellation := context.AfterFunc(signalCtx, cancelLifetime)
	defer stopStartupCancellation()
	shutdownTelemetry, err := telemetry.Setup(lifetime, cfg.ServiceName, cfg.OTelEnabled)
	if err != nil {
		return fmt.Errorf("initialize telemetry: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := shutdownTelemetry(ctx); err != nil {
			logger.Error("telemetry shutdown failed", "error", err)
		}
	}()
	pool, err := db.Open(signalCtx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	authenticator, err := newAuthenticator(lifetime, cfg)
	if err != nil {
		return err
	}
	stopStartupCancellation()
	if err := signalCtx.Err(); err != nil {
		return err
	}
	if cfg.AuthMode == "dev" {
		logger.Warn("development authentication enabled", "subject", cfg.DevSubject)
	}
	var draining atomic.Bool
	ready := func(ctx context.Context) error {
		if draining.Load() {
			return fmt.Errorf("shutting down")
		}
		return pool.Ping(ctx)
	}
	handler, _, err := NewHandler(cfg, logger, NewDependencies(pool, authenticator, ready))
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: cfg.RequestTimeout + 5*time.Second,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10,
		BaseContext: func(net.Listener) context.Context { return lifetime },
		ErrorLog:    slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	logger.Info("api listening", "address", listener.Addr().String(), "environment", cfg.Environment)
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-signalCtx.Done():
		draining.Store(true)
		logger.Info("draining requests")
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			cancelLifetime()
			_ = server.Close()
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		return nil
	}
}
