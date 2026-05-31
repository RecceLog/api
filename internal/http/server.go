package http

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	_ "github.com/RecceLog/api/docs" // side-effect: registers swag spec
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	httpSwagger "github.com/swaggo/http-swagger/v2"

	"github.com/RecceLog/api/internal/auth"
	"github.com/RecceLog/api/internal/notes"
	"github.com/RecceLog/api/internal/routes"
	"github.com/RecceLog/api/internal/storage/postgres"
	"github.com/RecceLog/api/internal/users"
)

// Server is the HTTP boundary. It composes per-aggregate services and uses
// the Transactor directly to wrap cross-aggregate writes (e.g. POST /routes
// persists a route + a note_set in a single transaction).
type Server struct {
	pool           *pgxpool.Pool
	tx             *postgres.Transactor
	routes         *routes.Service
	notes          *notes.Service
	users          *users.Service
	auth           *auth.Middleware
	allowedOrigins []string
}

// NewServer wires the Server. The pool is kept for the /health check; all
// real persistence flows through routes/notes services. auth protects the
// mutating endpoints; reads stay public. allowedOrigins is the CORS origin
// pattern list (e.g. "http://localhost:*").
func NewServer(
	pool *pgxpool.Pool,
	tx *postgres.Transactor,
	routesSvc *routes.Service,
	notesSvc *notes.Service,
	usersSvc *users.Service,
	authMW *auth.Middleware,
	allowedOrigins []string,
) *Server {
	return &Server{
		pool:           pool,
		tx:             tx,
		routes:         routesSvc,
		notes:          notesSvc,
		users:          usersSvc,
		auth:           authMW,
		allowedOrigins: allowedOrigins,
	}
}

// Router creates and configures a chi router.
// It sets up global middlewares and endpoint handlers.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)

	// CORS — placed before Logger so preflight OPTIONS responses don't pollute
	// the access log. `Latitude`/`Longitude` are listed explicitly because the
	// nearby-routes endpoint reads them as custom request headers and browsers
	// strip non-allowed headers from preflighted requests.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   s.allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Content-Type", "Authorization", "Latitude", "Longitude"},
		ExposedHeaders:   []string{"Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// - local dev / no proxy:
	r.Use(middleware.ClientIPFromRemoteAddr)
	// - Cloudflare:
	//   r.Use(middleware.ClientIPFromHeader("CF-Connecting-IP"))
	// - proxy with known CIDR:
	//   r.Use(middleware.ClientIPFromXFF("10.0.0.0/8"))
	// - N proxy with dynamic IP:
	//   r.Use(middleware.ClientIPFromXFFTrustedProxies(2))

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/health", s.handleHealth)
	r.Get("/swagger/*", httpSwagger.WrapHandler)

	r.Route("/v1", func(r chi.Router) {
		r.Route("/routes", func(r chi.Router) {
			// public reads
			r.Get("/", s.handleListRoutes)
			r.Get("/{id}", s.handleGetRoute)
			r.Get("/range/{radius}", s.handleListRoutesInRange)

			// authenticated writes
			r.Group(func(r chi.Router) {
				r.Use(s.auth.Authenticate)
				r.Post("/", s.handleCreateRoute)
				r.Delete("/{id}", s.handleDeleteRoute)
				r.Post("/{id}/notes", s.handleAddNoteSet)
				r.Post("/{id}/waypoints", s.handleAddWaypoint)
				r.Patch("/{id}/waypoints/{waypointID}", s.handleUpdateWaypoint)
				r.Delete("/{id}/waypoints/{waypointID}", s.handleDeleteWaypoint)
			})
		})
		r.Route("/users", func(r chi.Router) {
			// public read of a profile
			r.Get("/{id}", s.handleGetUser)

			// authenticated: the caller's own profile (static path wins over {id})
			r.Group(func(r chi.Router) {
				r.Use(s.auth.Authenticate)
				r.Get("/me", s.handleGetMe)
			})
		})
		r.Route("/notes", func(r chi.Router) {
			// public reads
			r.Get("/{setID}", s.handleGetNotes)

			// authenticated writes
			r.Group(func(r chi.Router) {
				r.Use(s.auth.Authenticate)
				r.Delete("/{setID}", s.handleDeleteNoteSet)
				r.Patch("/{setID}/{noteID}", s.handleUpdateNote)
				r.Delete("/{setID}/{noteID}", s.handleDeleteNote)
			})
		})
	})

	return r
}

// handleHealth is the health endpoint handler.
// It pings the database with a 2-second budget; degraded responses are 503.
//
// @Summary     Health check
// @Description Returns ok if the database responds within 2s, otherwise degraded.
// @Tags        health
// @Produce     json
// @Success     200  {object}  map[string]string  "{\"status\": \"ok\"}"
// @Failure     503  {object}  map[string]string  "{\"status\": \"degraded\"}"
// @Router      /health [get]
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	status, code := "ok", http.StatusOK
	if err := s.pool.Ping(ctx); err != nil {
		status, code = "degraded", http.StatusServiceUnavailable
	}

	writeJSON(w, code, map[string]string{"status": status})
}

// writeJSON is a helper function to return a JSON as response.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
