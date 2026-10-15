package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"red-token/internal/app"
	"red-token/internal/config"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server_failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	application, err := app.New(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("app_init_failed: %w", err)
	}
	defer application.Close()

	application.StartBackground(ctx)

	servers := []struct {
		name   string
		server *http.Server
	}{
		{"backend", &http.Server{Addr: cfg.ListenAddr, Handler: application.Handler()}},
		{"frontend", &http.Server{Addr: cfg.FrontendListenAddr, Handler: application.Handler()}},
	}
	serverErrors := make(chan error, len(servers))
	for _, listener := range servers {
		listener := listener
		go func() {
			if err := listener.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				serverErrors <- fmt.Errorf("%s listener (%s): %w", listener.name, listener.server.Addr, err)
			}
		}()
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		for _, listener := range servers {
			_ = listener.server.Shutdown(shutdownCtx)
		}
	}()

	slog.Info("backend_listening", "listen_addr", cfg.ListenAddr)
	slog.Info("frontend_listening", "listen_addr", cfg.FrontendListenAddr)
	slog.Info("application_started",
		"db_path", cfg.DBPath,
		"log_level", cfg.LogLevel,
	)

	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-serverErrors:
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	var shutdownErr error
	for _, listener := range servers {
		if err := listener.server.Shutdown(shutdownCtx); err != nil && shutdownErr == nil {
			shutdownErr = fmt.Errorf("shutdown %s listener: %w", listener.name, err)
		}
	}
	if serveErr != nil {
		return serveErr
	}
	return shutdownErr
}

func parseLogLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
