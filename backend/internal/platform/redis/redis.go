// Package redis creates the Redis client used by the service.
package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"

	goredis "github.com/redis/go-redis/v9"

	"github.com/mounikavari9/playflow/backend/internal/config"
)

// errRedacted replaces an error whose message would reveal the password.
var errRedacted = errors.New("connection failed (details redacted)")

// New creates a Redis client and verifies connectivity with a ping bounded by
// cfg.ConnectTimeout. The caller owns the returned client and must Close it.
//
// Errors never contain the connection URL or the password.
func New(ctx context.Context, cfg config.RedisConfig) (*goredis.Client, error) {
	opts, err := options(cfg)
	if err != nil {
		return nil, err
	}

	client := goredis.NewClient(opts)

	ctx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis: ping: %w", redact(err, opts.Password))
	}

	return client, nil
}

// options builds client options from cfg. Config validation already rejects
// query parameters; the TLS check is repeated here so that New stays safe when
// called with a RedisConfig that did not come from config.Load.
func options(cfg config.RedisConfig) (*goredis.Options, error) {
	opts, err := goredis.ParseURL(cfg.URL)
	if err != nil {
		// Not wrapped: url.Parse errors echo the whole URL, password included.
		return nil, errors.New("redis: invalid connection URL")
	}
	if opts.TLSConfig != nil && opts.TLSConfig.InsecureSkipVerify {
		return nil, errors.New("redis: TLS certificate verification cannot be disabled")
	}

	opts.PoolSize = cfg.PoolSize
	opts.DialTimeout = cfg.ConnectTimeout
	// Honor context deadlines on every command, so request-scoped timeouts
	// and cancellation reach Redis calls instead of only the client defaults.
	opts.ContextTimeoutEnabled = true

	return opts, nil
}

// redact returns err unless its message contains secret, in which case it
// returns an opaque error. go-redis dial and AUTH errors do not include the
// password today; this guards against a future driver or server change.
func redact(err error, secret string) error {
	if secret != "" && strings.Contains(err.Error(), secret) {
		return errRedacted
	}
	return err
}
