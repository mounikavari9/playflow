// Command healthcheck probes the API from inside its own container. The runtime
// image has no shell, curl or wget, so container health checks run this binary.
//
// Usage: healthcheck <path>, for example healthcheck /readyz. It exits 0 only
// when the API answers 200 on the port named by the container's PORT variable.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mounikavari9/playflow/backend/internal/config"
)

// requestTimeout bounds the whole probe. It must stay longer than the API's 2s
// readiness timeout and shorter than the orchestrator's health check timeout.
const requestTimeout = 3 * time.Second

// lookupFunc matches os.LookupEnv so tests can supply their own environment.
type lookupFunc func(key string) (string, bool)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.LookupEnv, os.Stderr))
}

// run performs the probe and returns the process exit code.
func run(ctx context.Context, args []string, lookup lookupFunc, stderr io.Writer) int {
	target, err := targetURL(args, lookup)
	if err == nil {
		err = check(ctx, target)
	}
	if err != nil {
		logger := slog.New(slog.NewJSONHandler(stderr, nil))
		logger.Error("healthcheck failed", slog.Any("error", err))
		return 1
	}
	return 0
}

// targetURL builds the loopback URL to probe from the arguments and PORT.
func targetURL(args []string, lookup lookupFunc) (string, error) {
	if len(args) != 1 {
		return "", errors.New("usage: healthcheck <path>")
	}
	path := args[0]
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "", errors.New("path must start with a single /")
	}

	port := config.DefaultPort
	if v, ok := lookup("PORT"); ok && v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 || p > 65535 {
			return "", errors.New("PORT: must be a number from 1 to 65535")
		}
		port = p
	}
	return fmt.Sprintf("http://127.0.0.1:%d%s", port, path), nil
}

// check requests target and returns an error unless the response status is 200.
// Redirects are not followed and proxy settings are ignored.
func check(ctx context.Context, target string) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	client := &http.Client{
		Transport: &http.Transport{DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}
