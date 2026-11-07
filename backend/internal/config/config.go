// Package config loads application configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"time"
)

// Environment names recognised by the service.
const (
	EnvLocal      = "local"
	EnvTest       = "test"
	EnvStaging    = "staging"
	EnvProduction = "production"
)

// Default values applied when the corresponding environment variable is unset.
const (
	DefaultPort             = 8080
	DefaultEnv              = EnvLocal
	DefaultShutdownTimeout  = 10 * time.Second
	DefaultDBMaxConns       = 10
	DefaultDBConnectTimeout = 5 * time.Second
)

// Config holds all runtime configuration for the API service.
type Config struct {
	// Port is the TCP port the HTTP server listens on (env: PORT).
	Port int
	// Env is the deployment environment (env: APP_ENV).
	Env string
	// ShutdownTimeout bounds graceful shutdown (env: SHUTDOWN_TIMEOUT, Go duration syntax).
	ShutdownTimeout time.Duration
	// Database holds PostgreSQL connection settings.
	Database DatabaseConfig
}

// DatabaseConfig holds PostgreSQL connection settings.
type DatabaseConfig struct {
	// URL is the PostgreSQL connection string (env: DATABASE_URL, required).
	// It contains credentials and must never be logged.
	URL string
	// MaxConns caps the connection pool size (env: DB_MAX_CONNS).
	MaxConns int32
	// ConnectTimeout bounds the initial connection and ping (env: DB_CONNECT_TIMEOUT).
	ConnectTimeout time.Duration
}

// LogValue implements slog.LogValuer so that logging the whole Config never
// exposes credentials. Database redaction is delegated to DatabaseConfig.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("port", c.Port),
		slog.String("env", c.Env),
		slog.Duration("shutdown_timeout", c.ShutdownTimeout),
		slog.Any("database", c.Database),
	)
}

// LogValue implements slog.LogValuer so that logging a DatabaseConfig never
// exposes the connection string or its credentials.
func (d DatabaseConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("url", "[REDACTED]"),
		slog.Int("max_conns", int(d.MaxConns)),
		slog.Duration("connect_timeout", d.ConnectTimeout),
	)
}

// Addr returns the listen address for the HTTP server.
func (c Config) Addr() string {
	return fmt.Sprintf(":%d", c.Port)
}

// IsLocal reports whether the service runs in the local development environment.
func (c Config) IsLocal() bool {
	return c.Env == EnvLocal
}

// Load reads configuration from the process environment.
func Load() (Config, error) {
	return load(os.LookupEnv)
}

// lookupFunc matches os.LookupEnv so tests can supply their own environment.
type lookupFunc func(key string) (string, bool)

func load(lookup lookupFunc) (Config, error) {
	cfg := Config{
		Port:            DefaultPort,
		Env:             DefaultEnv,
		ShutdownTimeout: DefaultShutdownTimeout,
		Database: DatabaseConfig{
			MaxConns:       DefaultDBMaxConns,
			ConnectTimeout: DefaultDBConnectTimeout,
		},
	}

	var errs []error

	if v, ok := lookup("PORT"); ok && v != "" {
		port, err := strconv.Atoi(v)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("PORT: %w", err))
		case port < 1 || port > 65535:
			errs = append(errs, fmt.Errorf("PORT: %d out of range 1-65535", port))
		default:
			cfg.Port = port
		}
	}

	if v, ok := lookup("APP_ENV"); ok && v != "" {
		switch v {
		case EnvLocal, EnvTest, EnvStaging, EnvProduction:
			cfg.Env = v
		default:
			errs = append(errs, fmt.Errorf("APP_ENV: unknown environment %q", v))
		}
	}

	if v, ok := lookup("SHUTDOWN_TIMEOUT"); ok && v != "" {
		d, err := time.ParseDuration(v)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("SHUTDOWN_TIMEOUT: %w", err))
		case d <= 0:
			errs = append(errs, fmt.Errorf("SHUTDOWN_TIMEOUT: must be positive, got %s", d))
		default:
			cfg.ShutdownTimeout = d
		}
	}

	if v, ok := lookup("DATABASE_URL"); !ok || v == "" {
		errs = append(errs, errors.New("DATABASE_URL: required"))
	} else if err := validateDatabaseURL(v); err != nil {
		errs = append(errs, fmt.Errorf("DATABASE_URL: %w", err))
	} else {
		cfg.Database.URL = v
	}

	if v, ok := lookup("DB_MAX_CONNS"); ok && v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("DB_MAX_CONNS: %w", err))
		case n < 1:
			errs = append(errs, fmt.Errorf("DB_MAX_CONNS: must be at least 1, got %d", n))
		default:
			cfg.Database.MaxConns = int32(n)
		}
	}

	if v, ok := lookup("DB_CONNECT_TIMEOUT"); ok && v != "" {
		d, err := time.ParseDuration(v)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("DB_CONNECT_TIMEOUT: %w", err))
		case d <= 0:
			errs = append(errs, fmt.Errorf("DB_CONNECT_TIMEOUT: must be positive, got %s", d))
		default:
			cfg.Database.ConnectTimeout = d
		}
	}

	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

// validateDatabaseURL checks the shape of a PostgreSQL URL. Its errors never
// include the URL itself, because it contains credentials; url.Parse errors
// embed the input, so they are deliberately not wrapped.
func validateDatabaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("not a valid URL")
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return fmt.Errorf("scheme must be postgres or postgresql, got %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("missing host")
	}
	return nil
}
