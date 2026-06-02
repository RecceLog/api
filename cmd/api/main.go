package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/RecceLog/api/internal/app"
	"github.com/RecceLog/api/internal/auth"
	"github.com/RecceLog/api/internal/config"
	apihttp "github.com/RecceLog/api/internal/http"
	"github.com/RecceLog/api/internal/mapbox"
	"github.com/RecceLog/api/internal/notes"
	"github.com/RecceLog/api/internal/routes"
	"github.com/RecceLog/api/internal/storage/postgres"
	"github.com/RecceLog/api/internal/users"
)

// main configures the logger and starts the server, handling graceful shutdown.
//
// @title                       RecceLog API
// @version                     0.1
// @description                 Routes, note sets and notes for the RecceLog application.
// @contact.name                RecceLog
// @license.name                Proprietary
// @host                        localhost:8080
// @BasePath                    /
// @schemes                     http https
// @accept                      json
// @produce                     json
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

	// per-aggregate repos + services, sharing one reentrant Transactor
	tx := postgres.NewTransactor(pool)

	// Mapbox geocoder: mandatory (the config rejects a missing token), used to
	// fill start/finish city names when the client omits them.
	geocoder := mapbox.NewHTTPGeocoder(cfg.Mapbox.AccessToken)

	routesSvc := routes.NewService(postgres.NewRoutes(pool), tx, geocoder)
	notesSvc := notes.NewService(postgres.NewNotes(pool), tx)
	usersSvc := users.NewService(postgres.NewUsers(pool))

	// application use-case layer: orchestrates cross-aggregate flows and owns
	// authorization, keeping the HTTP handlers as thin transport adapters.
	routeApp := app.NewRouteService(routesSvc, notesSvc, tx)
	noteApp := app.NewNoteService(notesSvc)
	userApp := app.NewUserService(usersSvc)

	// auth: validate Keycloak tokens (JWKS init is lazy, so the API boots even
	// if Keycloak is not yet reachable) and provision local users just-in-time.
	verifier := auth.NewOIDCVerifier(cfg.Auth.IssuerURL, cfg.Auth.DiscoveryURL, cfg.Auth.Audience)
	authMW := auth.NewMiddleware(verifier, usersSvc)

	// server configuration
	httpServer := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      apihttp.NewServer(pool, routeApp, noteApp, userApp, authMW, cfg.Server.AllowedOrigins, cfg.Server.RateLimitPerMin, cfg.Server.AvatarsDir).Router(),
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
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
