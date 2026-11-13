// Package health provides HTTP handlers for liveness and readiness probes.
package health

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Status values reported by health endpoints.
const (
	// StatusOK is the status value reported by a healthy probe.
	StatusOK = "ok"
	// StatusUnavailable is the status value reported when a dependency is not ready.
	StatusUnavailable = "unavailable"
)

// Failure categories logged when a readiness check fails. Raw dependency
// errors are never logged, because driver errors can name hosts and users.
const (
	failureTimeout     = "timeout"
	failureCanceled    = "canceled"
	failureUnavailable = "unavailable"
)

// Response is the JSON body returned by health endpoints.
type Response struct {
	Status string `json:"status"`
}

// Pinger reports whether a dependency is reachable. *pgxpool.Pool satisfies it.
//
// Implementations must honor ctx: Ping must return promptly once ctx is done.
// The readiness timeout is only enforced through ctx, so an implementation
// that ignores it can hold a probe open past the timeout.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Liveness reports that the process is up and serving HTTP.
//
// It deliberately checks no external dependencies: a database or cache outage
// must not cause the orchestrator to restart otherwise healthy pods. Dependency
// checks belong in the readiness probe.
func Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, Response{Status: StatusOK})
}

// Readiness returns a handler that reports whether the service can accept
// traffic, by pinging the database within timeout. It responds 200 when the
// ping succeeds and 503 otherwise.
//
// The response body never includes error details, and only a failure category
// (timeout, canceled or unavailable) is logged.
func Readiness(db Pinger, timeout time.Duration, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()

		err := db.Ping(ctx)
		if err == nil {
			// A ping that answered only after the deadline or a client
			// cancellation still fails, so the timeout is strict.
			err = ctx.Err()
		}
		if err != nil {
			logger.LogAttrs(c.Request.Context(), slog.LevelWarn, "readiness check failed",
				slog.String("dependency", "postgres"),
				slog.String("reason", failureReason(ctx)),
			)
			c.JSON(http.StatusServiceUnavailable, Response{Status: StatusUnavailable})
			return
		}

		c.JSON(http.StatusOK, Response{Status: StatusOK})
	}
}

// failureReason classifies a failed check from the state of its context rather
// than from the driver error, so the result does not depend on how the driver
// wraps context errors.
func failureReason(ctx context.Context) string {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return failureTimeout
	case errors.Is(ctx.Err(), context.Canceled):
		return failureCanceled
	default:
		return failureUnavailable
	}
}
