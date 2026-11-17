package config

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testDBURL    = "postgres://playflow:s3cret@localhost:5432/playflow?sslmode=disable"
	testRedisURL = "redis://:r3dis-pw@localhost:6379/0"
)

func envFrom(m map[string]string) lookupFunc {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
	}
}

// withRequired returns m plus a valid DATABASE_URL and REDIS_URL, unless m
// already sets them.
func withRequired(m map[string]string) map[string]string {
	out := map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func defaultDB() DatabaseConfig {
	return DatabaseConfig{
		URL:            testDBURL,
		MaxConns:       DefaultDBMaxConns,
		ConnectTimeout: DefaultDBConnectTimeout,
	}
}

func defaultRedis() RedisConfig {
	return RedisConfig{
		URL:            testRedisURL,
		PoolSize:       DefaultRedisPoolSize,
		ConnectTimeout: DefaultRedisConnectTimeout,
	}
}

func redisWithURL(u string) RedisConfig {
	r := defaultRedis()
	r.URL = u
	return r
}

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr string
	}{
		{
			name: "defaults when unset",
			env:  withRequired(nil),
			want: Config{Port: DefaultPort, Env: DefaultEnv, ShutdownTimeout: DefaultShutdownTimeout, Database: defaultDB(), Redis: defaultRedis()},
		},
		{
			name: "empty values fall back to defaults",
			env: withRequired(map[string]string{
				"PORT": "", "APP_ENV": "", "SHUTDOWN_TIMEOUT": "", "DB_MAX_CONNS": "", "DB_CONNECT_TIMEOUT": "",
			}),
			want: Config{Port: DefaultPort, Env: DefaultEnv, ShutdownTimeout: DefaultShutdownTimeout, Database: defaultDB(), Redis: defaultRedis()},
		},
		{
			name: "overrides",
			env: withRequired(map[string]string{
				"PORT": "9090", "APP_ENV": "production", "SHUTDOWN_TIMEOUT": "30s",
				"DB_MAX_CONNS": "25", "DB_CONNECT_TIMEOUT": "2s",
				"REDIS_URL": "rediss://app:pw@cache.internal:6380/2", "REDIS_POOL_SIZE": "20", "REDIS_CONNECT_TIMEOUT": "3s",
			}),
			want: Config{
				Port: 9090, Env: EnvProduction, ShutdownTimeout: 30 * time.Second,
				Database: DatabaseConfig{URL: testDBURL, MaxConns: 25, ConnectTimeout: 2 * time.Second},
				Redis:    RedisConfig{URL: "rediss://app:pw@cache.internal:6380/2", PoolSize: 20, ConnectTimeout: 3 * time.Second},
			},
		},
		{
			name: "postgresql scheme accepted",
			env:  withRequired(map[string]string{"DATABASE_URL": "postgresql://u:p@db:5432/x"}),
			want: Config{
				Port: DefaultPort, Env: DefaultEnv, ShutdownTimeout: DefaultShutdownTimeout,
				Database: DatabaseConfig{URL: "postgresql://u:p@db:5432/x", MaxConns: DefaultDBMaxConns, ConnectTimeout: DefaultDBConnectTimeout},
				Redis:    defaultRedis(),
			},
		},
		{
			name: "redis url without db or password",
			env:  withRequired(map[string]string{"REDIS_URL": "redis://localhost:6379"}),
			want: Config{Port: DefaultPort, Env: DefaultEnv, ShutdownTimeout: DefaultShutdownTimeout, Database: defaultDB(), Redis: redisWithURL("redis://localhost:6379")},
		},
		{
			name: "redis plaintext to loopback allowed in production",
			env:  withRequired(map[string]string{"APP_ENV": "production", "REDIS_URL": "redis://:pw@127.0.0.1:6380/0"}),
			want: Config{Port: DefaultPort, Env: EnvProduction, ShutdownTimeout: DefaultShutdownTimeout, Database: defaultDB(), Redis: redisWithURL("redis://:pw@127.0.0.1:6380/0")},
		},
		{
			name: "redis plaintext to ipv6 loopback allowed in staging",
			env:  withRequired(map[string]string{"APP_ENV": "staging", "REDIS_URL": "redis://:pw@[::1]:6380/0"}),
			want: Config{Port: DefaultPort, Env: EnvStaging, ShutdownTimeout: DefaultShutdownTimeout, Database: defaultDB(), Redis: redisWithURL("redis://:pw@[::1]:6380/0")},
		},
		{
			name: "redis plaintext to localhost allowed in production",
			env:  withRequired(map[string]string{"APP_ENV": "production", "REDIS_URL": "redis://:pw@localhost:6380"}),
			want: Config{Port: DefaultPort, Env: EnvProduction, ShutdownTimeout: DefaultShutdownTimeout, Database: defaultDB(), Redis: redisWithURL("redis://:pw@localhost:6380")},
		},
		{
			name: "redis plaintext to remote host allowed in test env",
			env:  withRequired(map[string]string{"APP_ENV": "test", "REDIS_URL": "redis://:pw@redis:6379/1"}),
			want: Config{Port: DefaultPort, Env: EnvTest, ShutdownTimeout: DefaultShutdownTimeout, Database: defaultDB(), Redis: redisWithURL("redis://:pw@redis:6379/1")},
		},
		{name: "non-numeric port", env: withRequired(map[string]string{"PORT": "abc"}), wantErr: "PORT"},
		{name: "port out of range", env: withRequired(map[string]string{"PORT": "70000"}), wantErr: "out of range"},
		{name: "unknown environment", env: withRequired(map[string]string{"APP_ENV": "prod"}), wantErr: `unknown environment "prod"`},
		{name: "invalid shutdown timeout", env: withRequired(map[string]string{"SHUTDOWN_TIMEOUT": "ten"}), wantErr: "SHUTDOWN_TIMEOUT"},
		{name: "non-positive shutdown timeout", env: withRequired(map[string]string{"SHUTDOWN_TIMEOUT": "0s"}), wantErr: "must be positive"},
		{name: "database url missing", env: map[string]string{}, wantErr: "DATABASE_URL: required"},
		{name: "database url empty", env: map[string]string{"DATABASE_URL": ""}, wantErr: "DATABASE_URL: required"},
		{name: "database url wrong scheme", env: map[string]string{"DATABASE_URL": "mysql://u:p@h/db"}, wantErr: "scheme must be postgres"},
		{name: "database url missing host", env: map[string]string{"DATABASE_URL": "postgres:///db"}, wantErr: "missing host"},
		{name: "database url unparseable", env: map[string]string{"DATABASE_URL": "postgres://u:p@h:bad port/db"}, wantErr: "not a valid URL"},
		{name: "max conns non-numeric", env: withRequired(map[string]string{"DB_MAX_CONNS": "many"}), wantErr: "DB_MAX_CONNS"},
		{name: "max conns zero", env: withRequired(map[string]string{"DB_MAX_CONNS": "0"}), wantErr: "must be at least 1"},
		{name: "max conns overflows int32", env: withRequired(map[string]string{"DB_MAX_CONNS": "3000000000"}), wantErr: "DB_MAX_CONNS"},
		{name: "connect timeout invalid", env: withRequired(map[string]string{"DB_CONNECT_TIMEOUT": "soon"}), wantErr: "DB_CONNECT_TIMEOUT"},
		{name: "connect timeout non-positive", env: withRequired(map[string]string{"DB_CONNECT_TIMEOUT": "-1s"}), wantErr: "DB_CONNECT_TIMEOUT: must be positive"},
		{name: "redis url missing", env: map[string]string{"DATABASE_URL": testDBURL}, wantErr: "REDIS_URL: required"},
		{name: "redis url empty", env: withRequired(map[string]string{"REDIS_URL": ""}), wantErr: "REDIS_URL: required"},
		{name: "redis url wrong scheme", env: withRequired(map[string]string{"REDIS_URL": "http://:pw@localhost:6379"}), wantErr: "scheme must be redis or rediss"},
		{name: "redis url unix scheme", env: withRequired(map[string]string{"REDIS_URL": "unix:///tmp/redis.sock"}), wantErr: "scheme must be redis or rediss"},
		{name: "redis url missing host", env: withRequired(map[string]string{"REDIS_URL": "redis:///0"}), wantErr: "REDIS_URL: missing host"},
		{name: "redis url unparseable", env: withRequired(map[string]string{"REDIS_URL": "redis://:pw@h:bad port/0"}), wantErr: "REDIS_URL: not a valid URL"},
		{name: "redis url skip_verify rejected", env: withRequired(map[string]string{"REDIS_URL": "rediss://:pw@cache:6379/0?skip_verify=true"}), wantErr: "query parameters are not supported"},
		{name: "redis url any query rejected", env: withRequired(map[string]string{"REDIS_URL": "redis://:pw@localhost:6379/0?pool_size=500"}), wantErr: "query parameters are not supported"},
		{name: "redis url empty query rejected", env: withRequired(map[string]string{"REDIS_URL": "redis://:pw@localhost:6379/0?"}), wantErr: "query parameters are not supported"},
		{name: "redis url non-numeric db", env: withRequired(map[string]string{"REDIS_URL": "redis://:pw@localhost:6379/cache"}), wantErr: "database number"},
		{name: "redis url negative db", env: withRequired(map[string]string{"REDIS_URL": "redis://:pw@localhost:6379/-1"}), wantErr: "database number"},
		{name: "redis url nested path", env: withRequired(map[string]string{"REDIS_URL": "redis://:pw@localhost:6379/0/1"}), wantErr: "database number"},
		{name: "redis plaintext remote in production", env: withRequired(map[string]string{"APP_ENV": "production", "REDIS_URL": "redis://:pw@cache.internal:6379/0"}), wantErr: "rediss:// (TLS) is required in production"},
		{name: "redis plaintext remote in staging", env: withRequired(map[string]string{"APP_ENV": "staging", "REDIS_URL": "redis://:pw@10.0.0.5:6379/0"}), wantErr: "rediss:// (TLS) is required in staging"},
		{name: "redis pool size non-numeric", env: withRequired(map[string]string{"REDIS_POOL_SIZE": "lots"}), wantErr: "REDIS_POOL_SIZE"},
		{name: "redis pool size zero", env: withRequired(map[string]string{"REDIS_POOL_SIZE": "0"}), wantErr: "REDIS_POOL_SIZE: must be at least 1"},
		{name: "redis pool size negative", env: withRequired(map[string]string{"REDIS_POOL_SIZE": "-3"}), wantErr: "REDIS_POOL_SIZE: must be at least 1"},
		{name: "redis connect timeout invalid", env: withRequired(map[string]string{"REDIS_CONNECT_TIMEOUT": "soon"}), wantErr: "REDIS_CONNECT_TIMEOUT"},
		{name: "redis connect timeout non-positive", env: withRequired(map[string]string{"REDIS_CONNECT_TIMEOUT": "0s"}), wantErr: "REDIS_CONNECT_TIMEOUT: must be positive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := load(envFrom(tt.env))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadReportsAllErrors(t *testing.T) {
	t.Parallel()

	_, err := load(envFrom(map[string]string{"PORT": "abc", "APP_ENV": "nope"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PORT")
	assert.Contains(t, err.Error(), "APP_ENV")
	assert.Contains(t, err.Error(), "DATABASE_URL")
	assert.Contains(t, err.Error(), "REDIS_URL")
}

func TestDatabaseURLErrorsOmitCredentials(t *testing.T) {
	t.Parallel()

	urls := []string{
		"mysql://user:hunter2@h/db",
		"postgres://user:hunter2@/db",
		"postgres://user:hunter2@h:bad port/db",
	}
	for _, u := range urls {
		_, err := load(envFrom(map[string]string{"DATABASE_URL": u}))
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "hunter2")
	}
}

func TestRedisURLErrorsOmitCredentials(t *testing.T) {
	t.Parallel()

	urls := []string{
		"http://user:hunter2@h:6379/0",
		"redis://user:hunter2@/0",
		"redis://user:hunter2@h:bad port/0",
		"rediss://user:hunter2@h:6379/0?skip_verify=hunter2",
		"redis://user:hunter2@h:6379/hunter2",
		"redis://user:hunter2@remote:6379/0",
	}
	for _, u := range urls {
		_, err := load(envFrom(map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": u, "APP_ENV": EnvProduction}))
		require.Error(t, err, u)
		assert.NotContains(t, err.Error(), "hunter2")
	}
}

func TestLogValueRedactsURLs(t *testing.T) {
	t.Parallel()

	subjects := map[string]any{
		"database config": defaultDB(),
		"redis config":    defaultRedis(),
		"full config":     Config{Port: DefaultPort, Env: DefaultEnv, ShutdownTimeout: DefaultShutdownTimeout, Database: defaultDB(), Redis: defaultRedis()},
	}
	handlers := map[string]func(*bytes.Buffer) slog.Handler{
		"json": func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) },
		"text": func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) },
	}
	for subjectName, subject := range subjects {
		for handlerName, newHandler := range handlers {
			t.Run(subjectName+"/"+handlerName, func(t *testing.T) {
				t.Parallel()

				var buf bytes.Buffer
				slog.New(newHandler(&buf)).Info("cfg", slog.Any("cfg", subject))

				out := buf.String()
				assert.NotContains(t, out, "s3cret")
				assert.NotContains(t, out, "localhost:5432")
				assert.NotContains(t, out, "r3dis-pw")
				assert.NotContains(t, out, "localhost:6379")
				assert.Contains(t, out, "[REDACTED]")
				assert.Contains(t, out, "connect_timeout")
			})
		}
	}
}

func TestConfigHelpers(t *testing.T) {
	t.Parallel()

	cfg := Config{Port: 8081, Env: EnvLocal}
	assert.Equal(t, ":8081", cfg.Addr())
	assert.True(t, cfg.IsLocal())

	cfg.Env = EnvStaging
	assert.False(t, cfg.IsLocal())
}
