package config

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testDBURL = "postgres://playflow:s3cret@localhost:5432/playflow?sslmode=disable"

func envFrom(m map[string]string) lookupFunc {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
	}
}

// withDB returns m plus a valid DATABASE_URL, unless m already sets one.
func withDB(m map[string]string) map[string]string {
	out := map[string]string{"DATABASE_URL": testDBURL}
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
			env:  withDB(nil),
			want: Config{Port: DefaultPort, Env: DefaultEnv, ShutdownTimeout: DefaultShutdownTimeout, Database: defaultDB()},
		},
		{
			name: "empty values fall back to defaults",
			env: withDB(map[string]string{
				"PORT": "", "APP_ENV": "", "SHUTDOWN_TIMEOUT": "", "DB_MAX_CONNS": "", "DB_CONNECT_TIMEOUT": "",
			}),
			want: Config{Port: DefaultPort, Env: DefaultEnv, ShutdownTimeout: DefaultShutdownTimeout, Database: defaultDB()},
		},
		{
			name: "overrides",
			env: withDB(map[string]string{
				"PORT": "9090", "APP_ENV": "production", "SHUTDOWN_TIMEOUT": "30s",
				"DB_MAX_CONNS": "25", "DB_CONNECT_TIMEOUT": "2s",
			}),
			want: Config{
				Port: 9090, Env: EnvProduction, ShutdownTimeout: 30 * time.Second,
				Database: DatabaseConfig{URL: testDBURL, MaxConns: 25, ConnectTimeout: 2 * time.Second},
			},
		},
		{
			name: "postgresql scheme accepted",
			env:  map[string]string{"DATABASE_URL": "postgresql://u:p@db:5432/x"},
			want: Config{
				Port: DefaultPort, Env: DefaultEnv, ShutdownTimeout: DefaultShutdownTimeout,
				Database: DatabaseConfig{URL: "postgresql://u:p@db:5432/x", MaxConns: DefaultDBMaxConns, ConnectTimeout: DefaultDBConnectTimeout},
			},
		},
		{name: "non-numeric port", env: withDB(map[string]string{"PORT": "abc"}), wantErr: "PORT"},
		{name: "port out of range", env: withDB(map[string]string{"PORT": "70000"}), wantErr: "out of range"},
		{name: "unknown environment", env: withDB(map[string]string{"APP_ENV": "prod"}), wantErr: `unknown environment "prod"`},
		{name: "invalid shutdown timeout", env: withDB(map[string]string{"SHUTDOWN_TIMEOUT": "ten"}), wantErr: "SHUTDOWN_TIMEOUT"},
		{name: "non-positive shutdown timeout", env: withDB(map[string]string{"SHUTDOWN_TIMEOUT": "0s"}), wantErr: "must be positive"},
		{name: "database url missing", env: map[string]string{}, wantErr: "DATABASE_URL: required"},
		{name: "database url empty", env: map[string]string{"DATABASE_URL": ""}, wantErr: "DATABASE_URL: required"},
		{name: "database url wrong scheme", env: map[string]string{"DATABASE_URL": "mysql://u:p@h/db"}, wantErr: "scheme must be postgres"},
		{name: "database url missing host", env: map[string]string{"DATABASE_URL": "postgres:///db"}, wantErr: "missing host"},
		{name: "database url unparseable", env: map[string]string{"DATABASE_URL": "postgres://u:p@h:bad port/db"}, wantErr: "not a valid URL"},
		{name: "max conns non-numeric", env: withDB(map[string]string{"DB_MAX_CONNS": "many"}), wantErr: "DB_MAX_CONNS"},
		{name: "max conns zero", env: withDB(map[string]string{"DB_MAX_CONNS": "0"}), wantErr: "must be at least 1"},
		{name: "max conns overflows int32", env: withDB(map[string]string{"DB_MAX_CONNS": "3000000000"}), wantErr: "DB_MAX_CONNS"},
		{name: "connect timeout invalid", env: withDB(map[string]string{"DB_CONNECT_TIMEOUT": "soon"}), wantErr: "DB_CONNECT_TIMEOUT"},
		{name: "connect timeout non-positive", env: withDB(map[string]string{"DB_CONNECT_TIMEOUT": "-1s"}), wantErr: "DB_CONNECT_TIMEOUT: must be positive"},
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

func TestLogValueRedactsDatabaseURL(t *testing.T) {
	t.Parallel()

	subjects := map[string]any{
		"database config": defaultDB(),
		"full config":     Config{Port: DefaultPort, Env: DefaultEnv, ShutdownTimeout: DefaultShutdownTimeout, Database: defaultDB()},
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
				assert.Contains(t, out, "[REDACTED]")
				assert.Contains(t, out, "max_conns")
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
