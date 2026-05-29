package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/RecceLog/api/internal/config"
	apihttp "github.com/RecceLog/api/internal/http"
	"github.com/RecceLog/api/internal/storage/postgres"
)

// main configures the logger and starts the server, handling graceful shutdown.
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("Start failed", "err", err)
		os.Exit(1)
	}
}

// run handles the configuration of graceful shutdowns, pool, httpServer and starts the server.
// having this function calling all the configuration functions assures the process that, in case an error occurs, all the deferred functions get called before the `os.Exit(1)`.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// context canceled on SIGINT/SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// connection pool configuration
	pool, err := postgres.NewPool(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer pool.Close()
	slog.Info("Connected to database")

	// server configuration
	httpServer := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      apihttp.NewServer(pool).Router(),
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// start listening in a goroutine that can write in the error channel `serverErr`
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("Server listening", "port", cfg.Server.Port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// listen for messages (errors or intentional shutdowns)
	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		slog.Info("Shutdown...")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}

	slog.Info("Server stopped correctly")
	return nil
}
