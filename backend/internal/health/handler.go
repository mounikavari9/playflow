// Package health provides HTTP handlers for liveness and readiness probes.
package health

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// StatusOK is the status value reported by a healthy probe.
const StatusOK = "ok"

// Response is the JSON body returned by health endpoints.
type Response struct {
	Status string `json:"status"`
}

// Liveness reports that the process is up and serving HTTP.
//
// It deliberately checks no external dependencies: a database or cache outage
// must not cause the orchestrator to restart otherwise healthy pods. Dependency
// checks belong in the readiness probe.
func Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, Response{Status: StatusOK})
}
