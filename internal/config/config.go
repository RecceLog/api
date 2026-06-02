package config

import (
	"errors"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains the entire application configuration structures
type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Auth     AuthConfig
	Mapbox   MapboxConfig
}

type ServerConfig struct {
	Port            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	// AllowedOrigins is the list of CORS Origin patterns the server will
	// allow (e.g. "http://localhost:*", "https://app.example.com"). Empty
	// means localhost-only.
	AllowedOrigins  []string
	RateLimitPerMin int
	// AvatarsDir is the directory holding the default avatar and the per-user
	// avatar files served by /v1/users/{id}/profile_pic.
	AvatarsDir string
}

type DatabaseConfig struct {
	DSN             string
	MaxConnections  int32
	MinConnections  int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

// MapboxConfig holds the credentials for server-side Mapbox API calls.
type MapboxConfig struct {
	// AccessToken is the Mapbox token used for server-side reverse geocoding of
	// start/finish cities. It is mandatory (see Load).
	AccessToken string
}

// AuthConfig holds the Keycloak/OIDC settings used to validate bearer tokens.
type AuthConfig struct {
	// IssuerURL is the public realm URL that must match the token `iss`
	// claim, e.g. "http://localhost:8081/realms/reccelog".
	IssuerURL string
	// DiscoveryURL is the URL the API uses to reach Keycloak for OIDC
	// discovery and JWKS (e.g. the in-network "http://keycloak:8080/realms/
	// reccelog"). Empty means "same as IssuerURL".
	DiscoveryURL string
	// Audience, when non-empty, is verified against the token `aud` claim.
	// Empty disables the audience check.
	Audience string
}

// Load reads environment variables and sets server and database configuration data or returns an error.
// Environment variables read are:
//   - DATABASE_CONNECTION_STRING: connection string to postgres db, if missing, the function will return an error
//   - API_PORT: exposed port of the api server, if missing, default value is "8080"
//   - API_READ_TIMEOUT: maximum time specified in seconds to read the request, if missing, default value is 5
//   - API_WRITE_TIMEOUT: maximum time specified in seconds to write the request, if missing, default value is 10
//   - API_IDLE_TIMEOUT: maximum keep-alive idle time before an idle connection is closed, if missing, default value is 60 seconds
//   - API_SHUTDOWN_TIMEOUT: maximum time specified in seconds to shut down the server, if missing, default value is 15 seconds
//   - API_RATE_LIMIT_PER_MIN: max requests per minute per client IP; 0 disables rate limiting; if missing, default value is 100
//   - API_ALLOWED_ORIGINS: comma-separated CORS origins (e.g. "https://app.example.com,https://staging.example.com"), if missing, defaults to "http://localhost:*,http://127.0.0.1:*"
//   - KEYCLOAK_ISSUER_URL: public realm URL that must match the token `iss` claim (e.g. "http://localhost:8081/realms/reccelog"), if missing, the function will return an error
//   - KEYCLOAK_DISCOVERY_URL: URL the API uses to reach Keycloak for OIDC discovery/JWKS (e.g. "http://keycloak:8080/realms/reccelog"), if missing, defaults to KEYCLOAK_ISSUER_URL
//   - KEYCLOAK_AUDIENCE: expected token `aud` claim; if missing, the audience check is skipped
//   - MAPBOX_ACCESS_TOKEN: server-side Mapbox token used to reverse-geocode start/finish cities; mandatory, the function returns an error if missing
//   - AVATARS_DIR: directory holding the default avatar and per-user avatar files; if missing, defaults to "./static/avatars"
//   - DB_MAX_CONNECTIONS: amount of maximum connection of the pool to the database, if missing, default value is 10
//   - DB_MIN_CONNECTIONS: amount of minimum connection of the pool to the database, if missing, default value is 0
//   - DB_MAX_CONN_LIFETIME: maximum time specified in hours that a connection of the pool can stay alive, if missing, default value is 1
//   - DB_MAX_CONN_IDLE_TIME: maximum time specified in minutes that a connection of the pool can stay in idle mode, if missing, default value is 30
func Load() (Config, error) {
	dsn := os.Getenv("DATABASE_CONNECTION_STRING")
	if dsn == "" {
		return Config{}, errors.New("missing DATABASE_CONNECTION_STRING environment variable")
	}

	port := os.Getenv("API_PORT")
	if port == "" {
		return Config{}, errors.New("missing API_PORT environment variable")
	}

	issuerURL := os.Getenv("KEYCLOAK_ISSUER_URL")
	if issuerURL == "" {
		return Config{}, errors.New("missing KEYCLOAK_ISSUER_URL environment variable")
	}

	// Mapbox is mandatory: start/finish city names are filled by reverse
	// geocoding when the client omits them, and the columns are NOT NULL.
	mapboxToken := os.Getenv("MAPBOX_ACCESS_TOKEN")
	if mapboxToken == "" {
		return Config{}, errors.New("missing MAPBOX_ACCESS_TOKEN environment variable")
	}

	allowedOrigins := []string{"http://localhost:*", "http://127.0.0.1:*"}
	if raw := os.Getenv("API_ALLOWED_ORIGINS"); raw != "" {
		allowedOrigins = splitAndTrim(raw)
	}

	return Config{
		Server: ServerConfig{
			Port:            port,
			ReadTimeout:     getEnv("API_READ_TIMEOUT", 5*time.Second),
			WriteTimeout:    getEnv("API_WRITE_TIMEOUT", 10*time.Second),
			IdleTimeout:     getEnv("API_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout: getEnv("API_SHUTDOWN_TIMEOUT", 15*time.Second),
			AllowedOrigins:  allowedOrigins,
			RateLimitPerMin: getEnv("API_RATE_LIMIT_PER_MIN", 100),
			AvatarsDir:      getEnv("AVATARS_DIR", "./static/avatars"),
		},
		Database: DatabaseConfig{
			DSN:             dsn,
			MaxConnections:  getEnv("DB_MAX_CONNECTIONS", int32(10)),
			MinConnections:  getEnv("DB_MIN_CONNECTIONS", int32(0)),
			MaxConnLifetime: getEnv("DB_MAX_CONN_LIFETIME", time.Hour),
			MaxConnIdleTime: getEnv("DB_MAX_CONN_IDLE_TIME", 30*time.Minute),
		},
		Auth: AuthConfig{
			IssuerURL:    issuerURL,
			DiscoveryURL: os.Getenv("KEYCLOAK_DISCOVERY_URL"),
			Audience:     os.Getenv("KEYCLOAK_AUDIENCE"),
		},
		Mapbox: MapboxConfig{
			AccessToken: mapboxToken,
		},
	}, nil
}

// splitAndTrim splits a comma-separated env var, trimming whitespace and
// dropping empty entries — so " a, b ,, c " yields []string{"a","b","c"}.
func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// envValue specifies variable types accepted for getEnv
type envValue interface {
	string | int | int32 | int64 | bool | float64 | time.Duration
}

// getEnv tries to read an environment variable named `key`, if missing the function returns `fallback`.
// variable types accepted for this function are `envValue`
func getEnv[T envValue](key string, fallback T) T {
	s, ok := os.LookupEnv(key)
	if !ok || s == "" {
		return fallback
	}

	var (
		parsed any
		err    error
	)

	switch any(fallback).(type) {
	case string:
		parsed = s
	case int:
		parsed, err = strconv.Atoi(s)
	case int32:
		var v int64
		v, err = strconv.ParseInt(s, 10, 32)
		parsed = int32(v)
	case int64:
		parsed, err = strconv.ParseInt(s, 10, 64)
	case bool:
		parsed, err = strconv.ParseBool(s)
	case float64:
		parsed, err = strconv.ParseFloat(s, 64)
	case time.Duration:
		parsed, err = time.ParseDuration(s)
	}

	if err != nil {
		slog.Warn("Environment variable not valid, using fallback",
			"key", key, "value", s, "err", err)
		return fallback
	}

	return parsed.(T)
}
