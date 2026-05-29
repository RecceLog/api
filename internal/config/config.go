package config

import (
	"errors"
	"log/slog"
	"os"
	"strconv"
	"time"
)

// Config contains the entire application configuration structures
type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
}

type ServerConfig struct {
	Port            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

type DatabaseConfig struct {
	DSN             string
	MaxConnections  int32
	MinConnections  int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

// Load reads environment variables and sets server and database configuration data or returns an error.
// Environment variables read are:
//   - DATABASE_CONNECTION_STRING: connection string to postgres db, if missing, the function will return an error
//   - API_PORT: exposed port of the api server, if missing, default value is "8080"
//   - API_READ_TIMEOUT: maximum time specified in seconds to read the request, if missing, default value is 5
//   - API_WRITE_TIMEOUT: maximum time specified in seconds to write the request, if missing, default value is 10
//   - API_SHUTDOWN_TIMEOUT: maximum time specified in seconds to shut down the server, if missing, default value is 15 seconds
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

	return Config{
		Server: ServerConfig{
			Port:            port,
			ReadTimeout:     getEnv("API_READ_TIMEOUT", 5*time.Second),
			WriteTimeout:    getEnv("API_WRITE_TIMEOUT", 10*time.Second),
			ShutdownTimeout: getEnv("API_SHUTDOWN_TIMEOUT", 15*time.Second),
		},
		Database: DatabaseConfig{
			DSN:             dsn,
			MaxConnections:  getEnv("DB_MAX_CONNECTIONS", int32(10)),
			MinConnections:  getEnv("DB_MIN_CONNECTIONS", int32(0)),
			MaxConnLifetime: getEnv("DB_MAX_CONN_LIFETIME", time.Hour),
			MaxConnIdleTime: getEnv("DB_MAX_CONN_IDLE_TIME", 30*time.Minute),
		},
	}, nil
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
