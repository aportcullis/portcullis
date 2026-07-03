// Package server wires the HTTP surface: health endpoints and the embedded SPA.
package server

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/aportcullis/portcullis/internal/platform/assets"
	"github.com/aportcullis/portcullis/internal/platform/health"
	"github.com/aportcullis/portcullis/internal/platform/logging"
)

// Server owns the HTTP listener and its lifecycle.
type Server struct {
	http       *http.Server
	health     *health.Handler
	logger     *slog.Logger
	drainDelay time.Duration
}

// Mount attaches an HTTP route — typically a Connect RPC handler — to the server.
type Mount struct {
	Pattern string
	Handler http.Handler
}

// New builds the server with health endpoints, the embedded frontend, and any
// provided API mounts. drainDelay is how long readiness reports "draining"
// before connections are closed, giving Kubernetes time to deregister the pod.
func New(addr string, logger *slog.Logger, drainDelay time.Duration, mounts ...Mount) *Server {
	h := health.New().WithLogger(logger)
	s := &Server{logger: logger, health: h, drainDelay: drainDelay}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", h.Live)
	mux.HandleFunc("GET /readyz", h.Ready)
	for _, m := range mounts {
		mux.Handle(m.Pattern, m.Handler)
	}
	mux.Handle("/", s.spa())

	s.http = &http.Server{
		Addr:              addr,
		Handler:           logging.Middleware(logger)(mux),
		ReadHeaderTimeout: 10 * time.Second,
		// Bound the whole request read so a slow-body (slowloris) connection can't
		// hold a goroutine open indefinitely, and reap idle keep-alives. WriteTimeout
		// is intentionally unset: once streaming RPCs land, a blanket write deadline
		// would kill long-lived streams — use per-request http.ResponseController
		// deadlines there instead.
		ReadTimeout: 30 * time.Second,
		IdleTimeout: 120 * time.Second,
	}
	return s
}

// Health exposes the readiness registry so dependencies (e.g. the metadata
// database) can register their own checks.
func (s *Server) Health() *health.Handler { return s.health }

// Handler returns the root HTTP handler (health, SPA, and—later—RPC). It is the
// entry point for in-process end-to-end tests.
func (s *Server) Handler() http.Handler { return s.http.Handler }

// ListenAndServe starts serving and blocks until the server stops.
func (s *Server) ListenAndServe() error { return s.http.ListenAndServe() }

// Shutdown sheds traffic, then gracefully drains in-flight requests. Readiness
// flips to draining first so Kubernetes stops routing before connections close.
func (s *Server) Shutdown(ctx context.Context) error {
	s.health.StartDraining()
	if s.drainDelay > 0 {
		s.logger.Info("draining", "delay", s.drainDelay)
		select {
		case <-time.After(s.drainDelay):
		case <-ctx.Done():
		}
	}
	return s.http.Shutdown(ctx)
}

// spa serves the embedded built frontend, falling back to index.html for
// client-side routes. Before the frontend is built it serves a placeholder.
func (s *Server) spa() http.Handler {
	dist, err := assets.Dist()
	if err != nil {
		s.logger.Warn("embedded frontend unavailable", "err", err)
		return http.HandlerFunc(frontendNotBuilt)
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return http.HandlerFunc(frontendNotBuilt)
	}

	fileServer := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" {
			if _, err := fs.Stat(dist, name); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// SPA fallback: serve index.html for unknown (client-routed) paths.
		clone := r.Clone(r.Context())
		clone.URL.Path = "/"
		fileServer.ServeHTTP(w, clone)
	})
}

func frontendNotBuilt(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `<!doctype html><html><body style="font-family:system-ui;margin:3rem">
<h1>Portcullis</h1>
<p>Frontend not built yet. Run <code>pnpm -C web install</code> then <code>make web</code>.</p>
</body></html>`)
}
