package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envOf returns a lookupFunc backed by a map.
func envOf(env map[string]string) lookupFunc {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

func TestTargetURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		want    string
		wantErr string
	}{
		{name: "default port", args: []string{"/readyz"}, want: "http://127.0.0.1:8080/readyz"},
		{name: "empty port uses default", args: []string{"/readyz"}, env: map[string]string{"PORT": ""}, want: "http://127.0.0.1:8080/readyz"},
		{name: "uses PORT", args: []string{"/healthz"}, env: map[string]string{"PORT": "9090"}, want: "http://127.0.0.1:9090/healthz"},
		{name: "ignores host API_PORT", args: []string{"/readyz"}, env: map[string]string{"PORT": "8080", "API_PORT": "18080"}, want: "http://127.0.0.1:8080/readyz"},
		{name: "no arguments", wantErr: "usage"},
		{name: "too many arguments", args: []string{"/readyz", "/healthz"}, wantErr: "usage"},
		{name: "relative path", args: []string{"readyz"}, wantErr: "path must start"},
		{name: "host-like path", args: []string{"//example.com/readyz"}, wantErr: "path must start"},
		{name: "non-numeric port", args: []string{"/readyz"}, env: map[string]string{"PORT": "http"}, wantErr: "PORT"},
		{name: "port too low", args: []string{"/readyz"}, env: map[string]string{"PORT": "0"}, wantErr: "PORT"},
		{name: "port too high", args: []string{"/readyz"}, env: map[string]string{"PORT": "65536"}, wantErr: "PORT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := targetURL(tt.args, envOf(tt.env))
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRunStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		want   int
	}{
		{name: "ok", status: http.StatusOK, want: 0},
		{name: "no content is not ok", status: http.StatusNoContent, want: 1},
		{name: "redirect is not followed", status: http.StatusFound, want: 1},
		{name: "not found", status: http.StatusNotFound, want: 1},
		{name: "unavailable", status: http.StatusServiceUnavailable, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/readyz", r.URL.Path)
				if tt.status == http.StatusFound {
					w.Header().Set("Location", "/healthz")
				}
				w.WriteHeader(tt.status)
			}))
			t.Cleanup(srv.Close)

			u, err := url.Parse(srv.URL)
			require.NoError(t, err)

			var stderr bytes.Buffer
			got := run(t.Context(), []string{"/readyz"}, envOf(map[string]string{"PORT": u.Port()}), &stderr)

			assert.Equal(t, tt.want, got)
			if tt.want == 0 {
				assert.Empty(t, stderr.String())
			} else {
				assert.Contains(t, stderr.String(), "healthcheck failed")
			}
		})
	}
}

func TestRunConnectionRefused(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.NotFoundHandler())
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	srv.Close()

	var stderr bytes.Buffer
	got := run(t.Context(), []string{"/readyz"}, envOf(map[string]string{"PORT": u.Port()}), &stderr)

	assert.Equal(t, 1, got)
	assert.Contains(t, stderr.String(), "healthcheck failed")
}

func TestRunInvalidArguments(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	got := run(t.Context(), nil, envOf(nil), &stderr)

	assert.Equal(t, 1, got)
	assert.Contains(t, stderr.String(), "usage")
}

func TestCheckHonorsContextDeadline(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	err := check(ctx, srv.URL+"/readyz")

	require.ErrorIs(t, err, context.DeadlineExceeded)
}
