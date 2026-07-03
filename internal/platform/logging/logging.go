// Package logging provides a lightweight structured logger (slog) and an HTTP
// middleware that records one line per request. It is built to log everything
// cheaply: leveled output (debug captures all), and a request line that never
// includes SQL, parameters, or credentials.
package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// New builds a logger at the given level ("debug"|"info"|"warn"|"error") and
// format ("json"|"text"). Unknown values fall back to info / json.
func New(level, format string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(strings.ToUpper(strings.TrimSpace(level)))); err != nil {
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}

	var h slog.Handler
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "text":
		h = slog.NewTextHandler(os.Stdout, opts)
	default:
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(h)
}

// RequestID returns the request id carried in ctx, or "" if absent.
func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// Middleware logs one structured line per request and propagates a request id.
// It records method, path, status, size, duration, request id, and the socket
// remote — never SQL, parameters, or secrets. Health probes are logged at debug
// so steady-state logs stay quiet while debug still captures everything.
func Middleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-Id")
			if id == "" {
				id = newID()
			}
			ctx := context.WithValue(r.Context(), requestIDKey, id)
			w.Header().Set("X-Request-Id", id)

			rec := &recorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, r.WithContext(ctx))

			level := slog.LevelInfo
			if isHealthPath(r.URL.Path) {
				level = slog.LevelDebug
			}
			logger.LogAttrs(ctx, level, "http",
				slog.String("request_id", id),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int64("bytes", rec.bytes),
				slog.Duration("duration", time.Since(start)),
				slog.String("remote", remoteIP(r.RemoteAddr)),
			)
		})
	}
}

func isHealthPath(p string) bool { return p == "/livez" || p == "/readyz" }

type recorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *recorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

// Flush forwards to the wrapped writer's Flusher. connect-go detects streaming
// support with a direct `w.(http.Flusher)` assertion (not http.ResponseController),
// so the recorder must implement Flush itself or server-streaming RPCs are rejected.
func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap exposes the wrapped writer so http.ResponseController can still reach the
// underlying Hijacker/deadline setters.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// remoteIP strips the port. Forwarded headers are intentionally ignored here;
// trusting them is decided at the proxy boundary, not in request logging.
func remoteIP(remoteAddr string) string {
	if i := strings.LastIndex(remoteAddr, ":"); i >= 0 {
		return remoteAddr[:i]
	}
	return remoteAddr
}
