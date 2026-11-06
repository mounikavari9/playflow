// Package config loads application configuration from environment variables.
package config

import (
	"errors"
	"fmt"
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
	DefaultPort            = 8080
	DefaultEnv             = EnvLocal
	DefaultShutdownTimeout = 10 * time.Second
)

// Config holds all runtime configuration for the API service.
type Config struct {
	// Port is the TCP port the HTTP server listens on (env: PORT).
	Port int
	// Env is the deployment environment (env: APP_ENV).
	Env string
	// ShutdownTimeout bounds graceful shutdown (env: SHUTDOWN_TIMEOUT, Go duration syntax).
	ShutdownTimeout time.Duration
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

	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}
