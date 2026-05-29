package http

import (
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

	return r
}
