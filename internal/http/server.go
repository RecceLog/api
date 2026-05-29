package http

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	pool *pgxpool.Pool
}

func NewServer(pool *pgxpool.Pool) *Server {
	return &Server{pool: pool}
}

// Router creates and configures a chi router.
// It sets up global middlewares and endpoint handlers.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)

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

	return r
}

// handleHealth is the health endpoint handler.
// It makes a request to the database, and if no response is received in 2 seconds, the api returns status 503, otherwise it returns status OK.
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
