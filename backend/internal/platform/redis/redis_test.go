package redis

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mounikavari9/playflow/backend/internal/config"
)

func testConfig(u string) config.RedisConfig {
	return config.RedisConfig{URL: u, PoolSize: 4, ConnectTimeout: 2 * time.Second}
}

func TestOptions(t *testing.T) {
	t.Parallel()

	t.Run("plaintext", func(t *testing.T) {
		t.Parallel()

		opts, err := options(testConfig("redis://app:hunter2@localhost:6380/3"))
		require.NoError(t, err)
		assert.Equal(t, "localhost:6380", opts.Addr)
		assert.Equal(t, "app", opts.Username)
		assert.Equal(t, "hunter2", opts.Password)
		assert.Equal(t, 3, opts.DB)
		assert.Equal(t, 4, opts.PoolSize)
		assert.Equal(t, 2*time.Second, opts.DialTimeout)
		assert.True(t, opts.ContextTimeoutEnabled)
		assert.Nil(t, opts.TLSConfig)
	})

	t.Run("tls verifies certificates", func(t *testing.T) {
		t.Parallel()

		opts, err := options(testConfig("rediss://:hunter2@cache.example.com:6379/0"))
		require.NoError(t, err)
		require.NotNil(t, opts.TLSConfig)
		assert.Equal(t, "cache.example.com", opts.TLSConfig.ServerName)
		assert.False(t, opts.TLSConfig.InsecureSkipVerify)
		assert.GreaterOrEqual(t, opts.TLSConfig.MinVersion, uint16(tls.VersionTLS12))
	})
}

func TestNewErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		wantErr string
	}{
		{
			name:    "unparseable URL",
			url:     "redis://user:hunter2@localhost:bad port/0",
			wantErr: "redis: invalid connection URL",
		},
		{
			name:    "unsupported scheme",
			url:     "http://user:hunter2@localhost:6379/0",
			wantErr: "redis: invalid connection URL",
		},
		{
			name:    "skip_verify rejected",
			url:     "rediss://user:hunter2@localhost:6379/0?skip_verify=true",
			wantErr: "TLS certificate verification cannot be disabled",
		},
		{
			name:    "unreachable server",
			url:     "redis://user:hunter2@127.0.0.1:1/0",
			wantErr: "redis: ping",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			start := time.Now()
			client, err := New(context.Background(), testConfig(tt.url))
			require.Error(t, err)
			assert.Nil(t, client)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.NotContains(t, err.Error(), "hunter2")
			assert.Less(t, time.Since(start), 3*time.Second, "must respect ConnectTimeout")
		})
	}
}

func TestNewCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client, err := New(ctx, testConfig("redis://:hunter2@127.0.0.1:1/0"))
	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "redis: ping")
}

func TestRedact(t *testing.T) {
	t.Parallel()

	plain := errors.New("dial tcp 10.0.0.1:6379: connect: connection refused")
	leaky := errors.New("AUTH hunter2 rejected")

	tests := []struct {
		name   string
		err    error
		secret string
		want   error
	}{
		{name: "no secret in message", err: plain, secret: "hunter2", want: plain},
		{name: "secret in message", err: leaky, secret: "hunter2", want: errRedacted},
		{name: "secret wrapped", err: fmt.Errorf("outer: %w", leaky), secret: "hunter2", want: errRedacted},
		{name: "empty secret", err: leaky, secret: "", want: leaky},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, redact(tt.err, tt.secret))
		})
	}
}

// integrationURL returns TEST_REDIS_URL or skips the test. It needs a running
// Redis, e.g. TEST_REDIS_URL=redis://:playflow@localhost:6379/0
func integrationURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("TEST_REDIS_URL")
	if u == "" {
		t.Skip("TEST_REDIS_URL not set")
	}
	return u
}

func TestNewIntegration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client, err := New(ctx, testConfig(integrationURL(t)))
	require.NoError(t, err)
	// Cleanups run last-in, first-out: the key cleanup registered below runs
	// first, then the client closes, even when an assertion fails.
	t.Cleanup(func() { _ = client.Close() })

	assert.Equal(t, 4, client.Options().PoolSize)
	require.NoError(t, client.Ping(ctx).Err())

	key := fmt.Sprintf("playflow:test:%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = client.Del(context.Background(), key).Err() })

	// The short TTL is a fallback in case the cleanup cannot run.
	require.NoError(t, client.Set(ctx, key, "v", 10*time.Second).Err())
	got, err := client.Get(ctx, key).Result()
	require.NoError(t, err)
	assert.Equal(t, "v", got)

	ttl, err := client.TTL(ctx, key).Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, time.Duration(0))
}

func TestNewIntegrationClose(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client, err := New(ctx, testConfig(integrationURL(t)))
	require.NoError(t, err)

	require.NoError(t, client.Close())
	assert.ErrorIs(t, client.Ping(ctx).Err(), goredis.ErrClosed)
}

func TestNewIntegrationWrongPassword(t *testing.T) {
	t.Parallel()

	u, err := url.Parse(integrationURL(t))
	require.NoError(t, err)
	const wrong = "definitely-not-the-password"
	u.User = url.UserPassword(u.User.Username(), wrong)

	client, err := New(context.Background(), testConfig(u.String()))
	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "redis: ping")
	assert.NotContains(t, err.Error(), wrong)
	t.Logf("error: %v", err)
}
