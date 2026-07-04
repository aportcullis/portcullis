// Package health provides Kubernetes-style liveness and readiness endpoints.
//
// Liveness reflects only that the process is running; it must not check
// dependencies, because a failing liveness probe restarts the pod. Readiness
// reflects dependencies and drain state, so a down dependency (or a shutting-down
// process) sheds traffic without a restart.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Handler serves the liveness and readiness endpoints and owns the check registry.
type Handler struct {
	mu       sync.RWMutex
	checks   map[string]CheckFunc
	draining atomic.Bool
	timeout  time.Duration
	logger   *slog.Logger
}

// New returns a Handler with no checks registered and a default per-probe timeout.
func New() *Handler {
	return &Handler{checks: map[string]CheckFunc{}, timeout: 2 * time.Second, logger: slog.Default()}
}

// WithLogger sets the logger that records the detail of a failing readiness check
// server-side (the unauthenticated response only ever says "unavailable"). Returns
// the handler for chaining.
func (h *Handler) WithLogger(l *slog.Logger) *Handler {
	if l != nil {
		h.logger = l
	}
	return h
}

// Register adds a named readiness check (e.g. the metadata database).
// Registering the same name replaces the previous check.
func (h *Handler) Register(name string, c CheckFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks[name] = c
}

// StartDraining marks the process as shutting down so readiness fails and
// Kubernetes removes the pod from service endpoints before connections close.
func (h *Handler) StartDraining() { h.draining.Store(true) }

// Live always returns 200 while the process can serve HTTP.
func (h *Handler) Live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, response{Status: "ok"})
}

// Ready returns 200 only when not draining and every registered check passes;
// otherwise 503 with a per-check breakdown.
func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	if h.draining.Load() {
		writeJSON(w, http.StatusServiceUnavailable, response{Status: "draining"})
		return
	}

	h.mu.RLock()
	checks := make(map[string]CheckFunc, len(h.checks))
	for n, c := range h.checks {
		checks[n] = c
	}
	timeout := h.timeout
	h.mu.RUnlock()

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	results := make(map[string]string, len(checks))
	ready := true
	for name, c := range checks {
		if err := c(ctx); err != nil {
			ready = false
			// Log only the error TYPE, never its message: a dependency error (e.g. a
			// DB connect failure) can echo a DSN/password, and even server-side logs
			// must stay secret-free (OWASP). The type names the category; the response
			// body only says "unavailable".
			h.logger.WarnContext(ctx, "readiness check failed", "check", name, "error_type", fmt.Sprintf("%T", err))
			results[name] = "unavailable"
			continue
		}
		results[name] = "ok"
	}

	if !ready {
		writeJSON(w, http.StatusServiceUnavailable, response{Status: "unavailable", Checks: results})
		return
	}
	writeJSON(w, http.StatusOK, response{Status: "ok", Checks: results})
}

func writeJSON(w http.ResponseWriter, status int, v response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
