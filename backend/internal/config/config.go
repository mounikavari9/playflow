// Package config loads application configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
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
	DefaultPort                = 8080
	DefaultEnv                 = EnvLocal
	DefaultShutdownTimeout     = 10 * time.Second
	DefaultDBMaxConns          = 10
	DefaultDBConnectTimeout    = 5 * time.Second
	DefaultRedisPoolSize       = 10
	DefaultRedisConnectTimeout = 5 * time.Second
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
	// Redis holds Redis connection settings.
	Redis RedisConfig
}

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	// URL is the Redis connection URL (env: REDIS_URL, required), in the form
	// redis[s]://[user]:password@host[:port][/db]. It contains credentials and
	// must never be logged. Query parameters are rejected, so options such as
	// skip_verify cannot be smuggled in. In staging and production it must use
	// rediss:// (TLS) unless the host is loopback, e.g. a TLS-terminating sidecar.
	URL string
	// PoolSize caps the number of connections (env: REDIS_POOL_SIZE).
	PoolSize int
	// ConnectTimeout bounds dialing and the startup ping (env: REDIS_CONNECT_TIMEOUT).
	ConnectTimeout time.Duration
}

// LogValue implements slog.LogValuer so that logging a RedisConfig never
// exposes the connection URL or its credentials.
func (r RedisConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("url", "[REDACTED]"),
		slog.Int("pool_size", r.PoolSize),
		slog.Duration("connect_timeout", r.ConnectTimeout),
	)
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
		slog.Any("redis", c.Redis),
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
		Redis: RedisConfig{
			PoolSize:       DefaultRedisPoolSize,
			ConnectTimeout: DefaultRedisConnectTimeout,
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

	// Runs after APP_ENV is parsed, because the TLS rule depends on it.
	if v, ok := lookup("REDIS_URL"); !ok || v == "" {
		errs = append(errs, errors.New("REDIS_URL: required"))
	} else if err := validateRedisURL(v, cfg.Env); err != nil {
		errs = append(errs, fmt.Errorf("REDIS_URL: %w", err))
	} else {
		cfg.Redis.URL = v
	}

	if v, ok := lookup("REDIS_POOL_SIZE"); ok && v != "" {
		n, err := strconv.Atoi(v)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("REDIS_POOL_SIZE: %w", err))
		case n < 1:
			errs = append(errs, fmt.Errorf("REDIS_POOL_SIZE: must be at least 1, got %d", n))
		default:
			cfg.Redis.PoolSize = n
		}
	}

	if v, ok := lookup("REDIS_CONNECT_TIMEOUT"); ok && v != "" {
		d, err := time.ParseDuration(v)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("REDIS_CONNECT_TIMEOUT: %w", err))
		case d <= 0:
			errs = append(errs, fmt.Errorf("REDIS_CONNECT_TIMEOUT: must be positive, got %s", d))
		default:
			cfg.Redis.ConnectTimeout = d
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

// validateRedisURL checks the shape and transport security of a Redis URL.
// Like validateDatabaseURL, its errors never include the URL or any part of it
// that could carry credentials.
func validateRedisURL(raw, env string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("not a valid URL")
	}
	if u.Scheme != "redis" && u.Scheme != "rediss" {
		return fmt.Errorf("scheme must be redis or rediss, got %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("missing host")
	}
	// go-redis turns query parameters into client options, including
	// skip_verify, which disables TLS certificate verification.
	if u.RawQuery != "" || u.ForceQuery {
		return errors.New("query parameters are not supported")
	}
	if p := strings.Trim(u.Path, "/"); p != "" {
		if db, err := strconv.Atoi(p); err != nil || db < 0 {
			return errors.New("path must be a non-negative database number, e.g. /0")
		}
	}
	if (env == EnvStaging || env == EnvProduction) && u.Scheme != "rediss" && !isLoopback(host) {
		return fmt.Errorf("rediss:// (TLS) is required in %s unless the host is loopback", env)
	}
	return nil
}

// isLoopback reports whether host names the local machine, where plaintext is
// acceptable because a sidecar proxy can provide TLS on the network hop.
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
