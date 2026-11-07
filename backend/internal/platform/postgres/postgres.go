// Package postgres creates the PostgreSQL connection pool used by the service.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mounikavari9/playflow/backend/internal/config"
)

// New creates a connection pool and verifies connectivity with a ping bounded
// by cfg.ConnectTimeout. The caller owns the returned pool and must Close it.
//
// Errors never contain the connection string, because it holds credentials.
func New(ctx context.Context, cfg config.DatabaseConfig) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		// Not wrapped: parse errors may echo parts of the connection string.
		return nil, errors.New("postgres: invalid connection string")
	}
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.ConnConfig.ConnectTimeout = cfg.ConnectTimeout

	ctx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}

	return pool, nil
}
