// Command api runs the PlayFlow HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"

	"github.com/mounikavari9/playflow/backend/internal/config"
	"github.com/mounikavari9/playflow/backend/internal/platform/postgres"
	"github.com/mounikavari9/playflow/backend/internal/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("api exited with error", slog.Any("error", err))
		os.Exit(1)
	}
}

// run wires dependencies, starts the server and blocks until a shutdown
// signal arrives or the server fails.
func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if !cfg.IsLocal() {
		gin.SetMode(gin.ReleaseMode)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Fail fast if the database is unreachable. pool.Close is deferred, so it
	// runs when run returns: after the srv.Shutdown call below has drained HTTP.
	pool, err := postgres.New(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer pool.Close()
	connCfg := pool.Config().ConnConfig
	logger.Info("connected to postgres", slog.String("host", connCfg.Host), slog.String("database", connCfg.Database))

	srv := server.New(cfg, logger)

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("starting api", slog.String("addr", srv.Addr), slog.String("env", cfg.Env))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- fmt.Errorf("listen and serve: %w", err)
		}
		close(serverErr)
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	logger.Info("api stopped gracefully")
	return nil
}
