package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/RecceLog/api/internal/auth"
	"github.com/RecceLog/api/internal/domain"
)

// fakeVerifier returns programmed claims/errors for a raw token.
type fakeVerifier struct {
	claims auth.Claims
	err    error
}

func (f fakeVerifier) Verify(_ context.Context, _ string) (auth.Claims, error) {
	return f.claims, f.err
}

// fakeProvisioner records the args it was called with.
type fakeProvisioner struct {
	user    domain.User
	err     error
	gotSub  string
	gotName string
	calls   int
}

func (f *fakeProvisioner) EnsureProvisioned(_ context.Context, sub, name string) (domain.User, error) {
	f.calls++
	f.gotSub, f.gotName = sub, name
	return f.user, f.err
}

func TestAuthenticate(t *testing.T) {
	t.Run("rejects missing token with 401", func(t *testing.T) {
		mw := auth.NewMiddleware(fakeVerifier{}, &fakeProvisioner{})
		rec := serve(mw, "", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("rejects invalid token with 401", func(t *testing.T) {
		mw := auth.NewMiddleware(fakeVerifier{err: errors.New("bad sig")}, &fakeProvisioner{})
		rec := serve(mw, "Bearer xxx", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("provisions user and exposes it to next handler", func(t *testing.T) {
		want := domain.User{ID: uuid.New(), KeycloakSub: "sub-1", DisplayName: "Ada"}
		prov := &fakeProvisioner{user: want}
		mw := auth.NewMiddleware(
			fakeVerifier{claims: auth.Claims{Subject: "sub-1", Name: "Ada"}},
			prov,
		)

		var gotUser *domain.User
		var ok bool
		next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			gotUser, ok = auth.UserFrom(r.Context())
		})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/routes", nil)
		req.Header.Set("Authorization", "Bearer good")
		mw.Authenticate(next).ServeHTTP(rec, req)

		if !ok || gotUser == nil || gotUser.ID != want.ID {
			t.Fatalf("UserFrom = (%v, %v), want user %v", gotUser, ok, want.ID)
		}
		if prov.gotSub != "sub-1" || prov.gotName != "Ada" {
			t.Errorf("provisioner got (%q, %q), want (sub-1, Ada)", prov.gotSub, prov.gotName)
		}
	})

	t.Run("falls back to preferred_username then subject for display name", func(t *testing.T) {
		prov := &fakeProvisioner{}
		mw := auth.NewMiddleware(
			fakeVerifier{claims: auth.Claims{Subject: "sub-2", PreferredUsername: "ada99"}},
			prov,
		)
		serve(mw, "Bearer good", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		if prov.gotName != "ada99" {
			t.Errorf("display name = %q, want preferred_username ada99", prov.gotName)
		}
	})

	t.Run("propagates provisioning failure (does not call next)", func(t *testing.T) {
		prov := &fakeProvisioner{err: errors.New("db down")}
		mw := auth.NewMiddleware(fakeVerifier{claims: auth.Claims{Subject: "s"}}, prov)

		called := false
		next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/routes", nil)
		req.Header.Set("Authorization", "Bearer good")
		mw.Authenticate(next).ServeHTTP(rec, req)

		if called {
			t.Error("next handler was called despite provisioning failure")
		}
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
	})
}

// serve runs the middleware with the given Authorization header against a
// no-op (or provided) next handler and returns the recorder.
func serve(mw *auth.Middleware, authHeader string, next http.Handler) *httptest.ResponseRecorder {
	if next == nil {
		next = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/routes", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	mw.Authenticate(next).ServeHTTP(rec, req)
	return rec
}
