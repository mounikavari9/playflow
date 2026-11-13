// Package server builds the HTTP server, its middleware stack and routes.
package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mounikavari9/playflow/backend/internal/config"
	"github.com/mounikavari9/playflow/backend/internal/health"
)

// HTTP server timeouts. They protect against slow clients holding connections open.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
)

// readinessTimeout bounds the /readyz database check. An orchestrator's
// readiness probe timeout must be longer than this.
const readinessTimeout = 2 * time.Second

// New returns an *http.Server configured with PlayFlow's routes and middleware.
// db is checked by the readiness probe. The caller is responsible for starting
// and shutting the server down.
func New(cfg config.Config, logger *slog.Logger, db health.Pinger) *http.Server {
	return &http.Server{
		Addr:              cfg.Addr(),
		Handler:           NewRouter(logger, db),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
}

// NewRouter returns the Gin engine with all middleware and routes registered.
// db is checked by the readiness probe.
func NewRouter(logger *slog.Logger, db health.Pinger) *gin.Engine {
	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.Use(gin.Recovery(), requestLogger(logger))

	r.GET("/healthz", health.Liveness)
	r.GET("/readyz", health.Readiness(db, readinessTimeout, logger))

	return r
}

// requestLogger logs one structured line per request. Only the path is logged,
// never the query string, so sensitive parameters do not end up in logs.
func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		logger.LogAttrs(c.Request.Context(), slog.LevelInfo, "http request",
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", c.Writer.Status()),
			slog.Duration("latency_ns", time.Since(start)),
			slog.String("client_ip", c.ClientIP()),
		)
	}
}
