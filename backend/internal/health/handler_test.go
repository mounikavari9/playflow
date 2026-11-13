package health

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}

func TestLiveness(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/healthz", nil)

	Liveness(c)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	var body Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, Response{Status: StatusOK}, body)
}

// fakePinger returns err. When block is set it first waits until the context
// ends, so block with a nil err imitates a ping that answers too late.
// It records whether the context it received carried a deadline.
type fakePinger struct {
	err         error
	block       bool
	hadDeadline bool
}

func (f *fakePinger) Ping(ctx context.Context) error {
	_, f.hadDeadline = ctx.Deadline()
	if f.block {
		<-ctx.Done()
	}
	return f.err
}

// errLeaky imitates a driver error that names connection details.
var errLeaky = errors.New("failed to connect to `user=playflow password=secret host=db.internal`")

func TestReadiness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		pinger     *fakePinger
		timeout    time.Duration
		wantStatus int
		wantBody   Response
		wantReason string // empty: no warning expected
	}{
		{
			name:       "healthy",
			pinger:     &fakePinger{},
			timeout:    time.Second,
			wantStatus: http.StatusOK,
			wantBody:   Response{Status: StatusOK},
		},
		{
			name:       "unhealthy",
			pinger:     &fakePinger{err: errLeaky},
			timeout:    time.Second,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   Response{Status: StatusUnavailable},
			wantReason: failureUnavailable,
		},
		{
			name:       "timeout",
			pinger:     &fakePinger{block: true, err: context.DeadlineExceeded},
			timeout:    20 * time.Millisecond,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   Response{Status: StatusUnavailable},
			wantReason: failureTimeout,
		},
		{
			// Ping succeeds, but only after the deadline: still not ready.
			name:       "nil after deadline",
			pinger:     &fakePinger{block: true},
			timeout:    20 * time.Millisecond,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   Response{Status: StatusUnavailable},
			wantReason: failureTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))

			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/readyz", nil)

			start := time.Now()
			Readiness(tt.pinger, tt.timeout, logger)(c)
			elapsed := time.Since(start)

			assert.Less(t, elapsed, time.Second, "readiness check must not hang")
			assert.True(t, tt.pinger.hadDeadline, "ping context must carry a deadline")
			assert.Equal(t, tt.wantStatus, rec.Code)

			var body Response
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tt.wantBody, body)

			// Neither the response nor the logs may leak the raw driver error.
			for _, out := range []string{rec.Body.String(), logs.String()} {
				assert.NotContains(t, out, "secret")
				assert.NotContains(t, out, "playflow")
				assert.NotContains(t, out, "db.internal")
			}

			if tt.wantReason == "" {
				assert.Empty(t, logs.String())
				return
			}
			var entry map[string]any
			require.NoError(t, json.Unmarshal(logs.Bytes(), &entry))
			assert.Equal(t, "WARN", entry["level"])
			assert.Equal(t, "readiness check failed", entry["msg"])
			assert.Equal(t, "postgres", entry["dependency"])
			assert.Equal(t, tt.wantReason, entry["reason"])
			assert.NotContains(t, entry, "error")
		})
	}
}

func TestReadinessClientCanceled(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // client already gone

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/readyz", nil).WithContext(ctx)

	Readiness(&fakePinger{block: true, err: context.Canceled}, time.Second, logger)(c)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var entry map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &entry))
	assert.Equal(t, failureCanceled, entry["reason"])
}
