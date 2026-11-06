package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func envFrom(m map[string]string) lookupFunc {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
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
			env:  map[string]string{},
			want: Config{Port: DefaultPort, Env: DefaultEnv, ShutdownTimeout: DefaultShutdownTimeout},
		},
		{
			name: "empty values fall back to defaults",
			env:  map[string]string{"PORT": "", "APP_ENV": "", "SHUTDOWN_TIMEOUT": ""},
			want: Config{Port: DefaultPort, Env: DefaultEnv, ShutdownTimeout: DefaultShutdownTimeout},
		},
		{
			name: "overrides",
			env:  map[string]string{"PORT": "9090", "APP_ENV": "production", "SHUTDOWN_TIMEOUT": "30s"},
			want: Config{Port: 9090, Env: EnvProduction, ShutdownTimeout: 30 * time.Second},
		},
		{
			name:    "non-numeric port",
			env:     map[string]string{"PORT": "abc"},
			wantErr: "PORT",
		},
		{
			name:    "port out of range",
			env:     map[string]string{"PORT": "70000"},
			wantErr: "out of range",
		},
		{
			name:    "unknown environment",
			env:     map[string]string{"APP_ENV": "prod"},
			wantErr: `unknown environment "prod"`,
		},
		{
			name:    "invalid shutdown timeout",
			env:     map[string]string{"SHUTDOWN_TIMEOUT": "ten"},
			wantErr: "SHUTDOWN_TIMEOUT",
		},
		{
			name:    "non-positive shutdown timeout",
			env:     map[string]string{"SHUTDOWN_TIMEOUT": "0s"},
			wantErr: "must be positive",
		},
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
}

func TestConfigHelpers(t *testing.T) {
	t.Parallel()

	cfg := Config{Port: 8081, Env: EnvLocal}
	assert.Equal(t, ":8081", cfg.Addr())
	assert.True(t, cfg.IsLocal())

	cfg.Env = EnvStaging
	assert.False(t, cfg.IsLocal())
}
