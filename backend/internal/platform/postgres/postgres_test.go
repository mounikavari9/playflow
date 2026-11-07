package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mounikavari9/playflow/backend/internal/config"
)

func TestNewErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		wantErr string
	}{
		{
			name:    "unparseable connection string",
			url:     "postgres://user:hunter2@localhost:5432/db?sslmode=bogus",
			wantErr: "invalid connection string",
		},
		{
			name:    "unreachable server",
			url:     "postgres://user:hunter2@127.0.0.1:1/db?sslmode=disable",
			wantErr: "postgres: ping",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pool, err := New(context.Background(), config.DatabaseConfig{
				URL:            tt.url,
				MaxConns:       2,
				ConnectTimeout: 2 * time.Second,
			})
			require.Error(t, err)
			assert.Nil(t, pool)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.NotContains(t, err.Error(), "hunter2")
		})
	}
}

// TestNewIntegration needs a running PostgreSQL. It runs only when
// TEST_DATABASE_URL is set, e.g.
// TEST_DATABASE_URL=postgres://playflow:playflow@localhost:5432/playflow?sslmode=disable
func TestNewIntegration(t *testing.T) {
	t.Parallel()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := New(ctx, config.DatabaseConfig{URL: url, MaxConns: 3, ConnectTimeout: 5 * time.Second})
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	assert.EqualValues(t, 3, pool.Config().MaxConns)

	var one int
	require.NoError(t, pool.QueryRow(ctx, "SELECT 1").Scan(&one))
	assert.Equal(t, 1, one)
}
