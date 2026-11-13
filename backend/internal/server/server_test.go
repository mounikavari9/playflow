package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mounikavari9/playflow/backend/internal/config"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakePinger returns err from Ping and counts how often it was called.
type fakePinger struct {
	err   error
	calls atomic.Int32
}

func (f *fakePinger) Ping(context.Context) error {
	f.calls.Add(1)
	return f.err
}

func TestRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		method        string
		path          string
		pingErr       error
		wantStatus    int
		wantBody      string
		wantPingCalls int32
	}{
		{name: "healthz ok", method: http.MethodGet, path: "/healthz", wantStatus: http.StatusOK, wantBody: `{"status":"ok"}`},
		// Liveness must not touch the database, even when it is down.
		{name: "healthz ok while db down", method: http.MethodGet, path: "/healthz", pingErr: errors.New("down"), wantStatus: http.StatusOK, wantBody: `{"status":"ok"}`},
		{name: "healthz wrong method", method: http.MethodPost, path: "/healthz", wantStatus: http.StatusMethodNotAllowed},
		{name: "readyz ok", method: http.MethodGet, path: "/readyz", wantStatus: http.StatusOK, wantBody: `{"status":"ok"}`, wantPingCalls: 1},
		{name: "readyz db down", method: http.MethodGet, path: "/readyz", pingErr: errors.New("down"), wantStatus: http.StatusServiceUnavailable, wantBody: `{"status":"unavailable"}`, wantPingCalls: 1},
		{name: "readyz wrong method", method: http.MethodPost, path: "/readyz", wantStatus: http.StatusMethodNotAllowed},
		{name: "unknown route", method: http.MethodGet, path: "/does-not-exist", wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := &fakePinger{err: tt.pingErr}
			router := NewRouter(discardLogger(), db)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantBody != "" {
				assert.JSONEq(t, tt.wantBody, rec.Body.String())
			}
			assert.Equal(t, tt.wantPingCalls, db.calls.Load())
		})
	}
}

func TestRequestLoggerOmitsQueryString(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	router := NewRouter(logger, &fakePinger{})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz?token=secret", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "http request", entry["msg"])
	assert.Equal(t, "GET", entry["method"])
	assert.Equal(t, "/healthz", entry["path"])
	assert.EqualValues(t, http.StatusOK, entry["status"])
	assert.NotContains(t, buf.String(), "secret")
}

func TestNewConfiguresServer(t *testing.T) {
	t.Parallel()

	srv := New(config.Config{Port: 9999}, discardLogger(), &fakePinger{})

	assert.Equal(t, ":9999", srv.Addr)
	assert.NotNil(t, srv.Handler)
	assert.Equal(t, readHeaderTimeout, srv.ReadHeaderTimeout)
	assert.Equal(t, readTimeout, srv.ReadTimeout)
	assert.Equal(t, writeTimeout, srv.WriteTimeout)
	assert.Equal(t, idleTimeout, srv.IdleTimeout)
}
