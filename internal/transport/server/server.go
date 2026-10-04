// Package server wires the HTTP surface: health endpoints and the embedded SPA.
package server

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
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
	// cancelRequests ends every request context derived from the server's base context.
	cancelRequests       context.CancelFunc
	shutdownTimeoutHooks []func(context.Context)
}

// New builds the server with health endpoints, the embedded frontend, and any provided API mounts behind the Host and browser-origin boundary (ADR-0052).
func New(opts Options, mounts ...Mount) (*Server, error) {
	if opts.Logger == nil || opts.Hosts == nil {
		return nil, errors.New("server: logger and host policy are required")
	}
	healthHandler := health.New().WithLogger(opts.Logger)
	s := &Server{logger: opts.Logger, health: healthHandler, drainDelay: opts.DrainDelay}

	application := http.NewServeMux()
	for _, mount := range mounts {
		application.Handle(mount.Pattern, mount.Handler)
	}
	application.Handle("/", s.spa())
	guarded, err := newOriginGuard(application, opts.Hosts, opts.BrowserOriginRequiredPaths)
	if err != nil {
		return nil, err
	}

	// Health probes stay outside the Host boundary: kubelet and container probes address the pod IP, and the endpoints expose no application data.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", healthHandler.Live)
	mux.HandleFunc("GET /readyz", healthHandler.Ready)
	mux.Handle("/", guarded)

	requestsCtx, cancelRequests := context.WithCancel(context.Background())
	s.cancelRequests = cancelRequests
	s.http = &http.Server{
		Addr:              opts.Addr,
		BaseContext:       func(net.Listener) context.Context { return requestsCtx },
		Handler:           securityHeaders(logging.Middleware(opts.Logger)(mux)),
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    maxHeaderBytes,
		// Bound the whole request read so a slow-body (slowloris) connection can't hold a goroutine open indefinitely, and reap idle keep-alives. WriteTimeout is intentionally unset: once streaming RPCs land, a blanket write deadline would kill long-lived streams — use per-request http.ResponseController deadlines there instead.
		ReadTimeout: 30 * time.Second,
		IdleTimeout: 120 * time.Second,
	}
	return s, nil
}

// Health exposes the readiness registry so dependencies (e.g. the metadata database) can register their own checks.
func (s *Server) Health() *health.Handler { return s.health }

// Handler returns the root HTTP handler (health, SPA, and—later—RPC). It is the entry point for in-process end-to-end tests.
func (s *Server) Handler() http.Handler { return s.http.Handler }

// ListenAndServe starts serving and blocks until the server stops.
func (s *Server) ListenAndServe() error { return s.http.ListenAndServe() }

// Serve accepts connections on an existing listener and blocks until the server stops.
func (s *Server) Serve(listener net.Listener) error { return s.http.Serve(listener) }

// OnShutdownTimeout registers setup-time work that runs when in-flight requests outlive the shutdown timeout, before their request contexts are cancelled.
func (s *Server) OnShutdownTimeout(hook func(context.Context)) {
	s.shutdownTimeoutHooks = append(s.shutdownTimeoutHooks, hook)
}

// securityHeaders limits every fetch class to the embedded SPA's origin, allowing data: images and inline styles only (ADR-0010).
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}

// Shutdown marks readiness draining, waits for deregistration, then gives in-flight requests a separate full timeout. Requests that outlive it first get the registered shutdown-timeout hooks, then have their contexts cancelled. Caller cancellation may abort either phase (ADR-0010).
func (s *Server) Shutdown(ctx context.Context, timeout time.Duration) error {
	s.health.StartDraining()
	if s.drainDelay > 0 {
		s.logger.Info("draining", "delay", s.drainDelay)
		select {
		case <-time.After(s.drainDelay):
		case <-ctx.Done():
		}
	}
	drainCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := s.http.Shutdown(drainCtx)
	if err != nil {
		for _, hook := range s.shutdownTimeoutHooks {
			hook(context.WithoutCancel(ctx))
		}
		s.cancelRequests()
	}
	return err
}

// spa serves the embedded built frontend, falling back to index.html for client-side routes. Before the frontend is built it serves a placeholder.
func (s *Server) spa() http.Handler {
	dist, err := assets.Dist()
	if err != nil {
		s.logger.Warn("embedded frontend unavailable", "err", err)
		return http.HandlerFunc(frontendNotBuilt)
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return http.HandlerFunc(frontendNotBuilt)
	}
	return spaHandler(dist)
}

// spaHandler serves files from dist (which must contain index.html), falling back to index.html for anything that is not an existing regular file — client-side routes and directories alike.
func spaHandler(dist fs.FS) http.Handler {
	fileServer := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" {
			// Serve only real FILES directly: a directory (e.g. /assets/) would make http.FileServerFS render an auto-generated listing of the embedded bundle, so it falls through to the SPA fallback like any client route.
			if fi, err := fs.Stat(dist, name); err == nil && !fi.IsDir() {
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
