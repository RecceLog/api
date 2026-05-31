package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/RecceLog/api/internal/domain"
	"github.com/RecceLog/api/internal/http/problem"
)

// ctxKey is the unexported context key under which the authenticated user is
// stored — unexported so only this package can set it.
type ctxKey struct{}

// UserProvisioner is the slice of the users service the middleware depends on
// (consumer-owned interface). It maps a verified identity to a local user,
// creating it on first sight.
type UserProvisioner interface {
	EnsureProvisioned(ctx context.Context, keycloakSub, displayName string) (domain.User, error)
}

// Middleware authenticates requests: it verifies the bearer token, ensures a
// local user exists for the caller, and injects that user into the request
// context for downstream handlers.
type Middleware struct {
	verifier TokenVerifier
	users    UserProvisioner
}

// NewMiddleware wires the middleware to its verifier and provisioner.
func NewMiddleware(verifier TokenVerifier, users UserProvisioner) *Middleware {
	return &Middleware{verifier: verifier, users: users}
}

// Authenticate is the chi middleware. It rejects requests without a valid token
// (401) and, on success, runs just-in-time user provisioning before handing off
// to the next handler with the user available via UserFrom.
func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearerToken(r)
		if !ok {
			problem.Unauthorized(w, r, "missing or malformed bearer token")
			return
		}

		claims, err := m.verifier.Verify(r.Context(), raw)
		if err != nil {
			problem.Unauthorized(w, r, "invalid token")
			return
		}

		user, err := m.users.EnsureProvisioned(r.Context(), claims.Subject, displayName(claims))
		if err != nil {
			// provisioning failure (DB down, invalid claims) is not the client's
			// fault — let From map it (500, or 422 for invalid claims).
			problem.From(w, r, err)
			return
		}

		ctx := context.WithValue(r.Context(), ctxKey{}, &user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserFrom returns the authenticated user injected by Authenticate. The bool is
// false on unprotected routes (no middleware ran).
func UserFrom(ctx context.Context) (*domain.User, bool) {
	u, ok := ctx.Value(ctxKey{}).(*domain.User)
	return u, ok
}

// bearerToken extracts the token from an "Authorization: Bearer <token>"
// header, case-insensitive on the scheme.
func bearerToken(r *http.Request) (string, bool) {
	const prefix = "bearer "
	h := r.Header.Get("Authorization")
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(h[len(prefix):])
	return token, token != ""
}

// displayName picks the friendliest non-empty claim for the local profile,
// falling back to the subject so the user record is always valid.
func displayName(c Claims) string {
	switch {
	case c.Name != "":
		return c.Name
	case c.PreferredUsername != "":
		return c.PreferredUsername
	default:
		return c.Subject
	}
}
