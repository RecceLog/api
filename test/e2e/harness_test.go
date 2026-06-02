//go:build integration

// Package e2e holds black-box integration tests: they boot the real router
// against a throwaway PostGIS container (via testcontainers) with the schema
// applied by goose, and drive it over HTTP exactly like a client would. The
// token verifier is faked, so no Keycloak is needed — a request with
// `Authorization: Bearer <name>` is authenticated as the user whose Keycloak
// subject is `<name>` (and `bad`/empty is rejected as 401).
//
// Run with:  go test -tags=integration ./test/e2e/...
package e2e

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver for goose
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/RecceLog/api/internal/app"
	"github.com/RecceLog/api/internal/auth"
	apihttp "github.com/RecceLog/api/internal/http"
	"github.com/RecceLog/api/internal/notes"
	"github.com/RecceLog/api/internal/routes"
	"github.com/RecceLog/api/internal/storage/postgres"
	"github.com/RecceLog/api/internal/users"
)

// baseURL is the address of the httptest server, set up once in TestMain.
var baseURL string

func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e setup failed:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

// run owns setup/teardown so deferred cleanups execute before os.Exit.
func run(m *testing.M) (int, error) {
	ctx := context.Background()

	pgC, err := tcpg.Run(ctx, "postgis/postgis:18-3.6",
		tcpg.WithDatabase("reccelog_test"),
		tcpg.WithUsername("test"),
		tcpg.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		return 1, fmt.Errorf("start postgis container: %w", err)
	}
	defer func() { _ = pgC.Terminate(ctx) }()

	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return 1, fmt.Errorf("connection string: %w", err)
	}

	if err := migrate(dsn); err != nil {
		return 1, fmt.Errorf("migrate: %w", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return 1, fmt.Errorf("pool: %w", err)
	}
	defer pool.Close()

	ts := httptest.NewServer(buildRouter(pool))
	defer ts.Close()
	baseURL = ts.URL

	return m.Run(), nil
}

// migrate applies the goose migrations using a database/sql connection (goose's
// API), separate from the pgx pool the app uses.
func migrate(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	goose.SetBaseFS(nil)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(db, filepath.Join(repoRoot(), "migrations"))
}

// buildRouter wires the real stack: a nil geocoder makes EnrichCities fall back
// to the "N/A" placeholder (deterministic), and the fake verifier removes the
// Keycloak dependency. Rate limiting is disabled.
func buildRouter(pool *pgxpool.Pool) http.Handler {
	tx := postgres.NewTransactor(pool)
	routesSvc := routes.NewService(postgres.NewRoutes(pool), tx, nil)
	notesSvc := notes.NewService(postgres.NewNotes(pool), tx)
	usersSvc := users.NewService(postgres.NewUsers(pool))

	routeApp := app.NewRouteService(routesSvc, notesSvc, tx)
	noteApp := app.NewNoteService(notesSvc)
	userApp := app.NewUserService(usersSvc)

	authMW := auth.NewMiddleware(fakeVerifier{}, usersSvc)

	avatarsDir := filepath.Join(repoRoot(), "static", "avatars")
	return apihttp.NewServer(pool, routeApp, noteApp, userApp, authMW, []string{"*"}, 0, avatarsDir).Router()
}

// repoRoot returns the api module root (this file lives at api/test/e2e/).
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// fakeVerifier authenticates a request as the user named by the raw token.
type fakeVerifier struct{}

func (fakeVerifier) Verify(_ context.Context, raw string) (auth.Claims, error) {
	if raw == "" || raw == "bad" {
		return auth.Claims{}, fmt.Errorf("invalid token")
	}
	return auth.Claims{Subject: raw, PreferredUsername: raw, Name: raw}, nil
}

// ----- HTTP helpers ---------------------------------------------------------

type response struct {
	status int
	body   []byte
}

// decode unmarshals the JSON body into v (fatal on error).
func (r response) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode body: %v (body=%s)", err, r.body)
	}
}

// req sends an HTTP request. body may be nil (no body), a string (sent raw —
// for malformed-JSON cases), or any value (JSON-encoded). token, when non-empty,
// is sent as a Bearer token. extraHeaders are applied last.
func req(t *testing.T, method, path, token string, body any, extraHeaders map[string]string) response {
	t.Helper()

	var reader io.Reader
	hasBody := body != nil
	switch b := body.(type) {
	case nil:
	case string:
		reader = bytes.NewReader([]byte(b))
	default:
		buf, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(buf)
	}

	r, err := http.NewRequest(method, baseURL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if hasBody {
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range extraHeaders {
		r.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return response{status: resp.StatusCode, body: b}
}

// assertStatus fails the test when the status is unexpected, printing the body.
func assertStatus(t *testing.T, r response, want int) {
	t.Helper()
	if r.status != want {
		t.Fatalf("status = %d, want %d (body=%s)", r.status, want, r.body)
	}
}

// ----- fixtures -------------------------------------------------------------

// validCreateBody is a minimal valid POST /v1/routes payload.
func validCreateBody() map[string]any {
	return map[string]any{
		"route": map[string]any{
			"name":     "Test Route",
			"vehicles": []string{"CAR"},
			"path": []map[string]float64{
				{"lng": 9.19, "lat": 45.46},
				{"lng": 12.49, "lat": 41.90},
			},
		},
		"note_set": map[string]any{
			"name": "Set",
			"notes": []map[string]any{
				{"order": 1, "type": "WARNING", "position": map[string]float64{"lng": 9.19, "lat": 45.46}},
			},
		},
	}
}

// createRoute creates a route as the given user and returns the detail response.
func createRoute(t *testing.T, token string) map[string]any {
	t.Helper()
	r := req(t, http.MethodPost, "/v1/routes", token, validCreateBody(), nil)
	assertStatus(t, r, http.StatusCreated)
	var out map[string]any
	r.decode(t, &out)
	return out
}
