// Package logging provides a lightweight structured logger (slog) and an HTTP middleware that records one line per request. It is built to log everything cheaply: leveled output (debug captures all), and a request line that never includes SQL, parameters, or credentials.
package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// levels is the supported log-level vocabulary — the single source of truth shared by New and by config validation via ParseLevel.
var levels = map[string]slog.Level{
	"debug": slog.LevelDebug,
	"info":  slog.LevelInfo,
	"warn":  slog.LevelWarn,
	"error": slog.LevelError,
}

// ParseLevel resolves a config value to its slog level. This package owns the supported vocabulary ("debug"|"info"|"warn"|"error", case-insensitive, surrounding space ignored); config validation delegates here so the accepted values and the logger's behavior can't drift apart.
func ParseLevel(level string) (slog.Level, bool) {
	lvl, ok := levels[strings.ToLower(strings.TrimSpace(level))]
	return lvl, ok
}

// parseFormat shares normalization between validation and logger construction; unknown formats return the JSON fallback and false.
func parseFormat(format string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case formatText:
		return formatText, true
	case formatJSON:
		return formatJSON, true
	default:
		return formatJSON, false
	}
}

// ValidFormat reports whether format names a supported handler ("json"|"text", case-insensitive, surrounding space ignored).
func ValidFormat(format string) bool {
	_, ok := parseFormat(format)
	return ok
}

// New builds a logger at the given level ("debug"|"info"|"warn"|"error") and format ("json"|"text"). Unknown values fall back to info / json — config.Load pre-validates via ParseLevel/ValidFormat, so the fallback only serves direct callers (tests, tools) that skip config.
func New(level, format string) *slog.Logger {
	lvl, ok := ParseLevel(level)
	if !ok {
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}

	f, _ := parseFormat(format)
	var h slog.Handler
	if f == formatText {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
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

// Middleware logs one structured line per request and propagates a request id. It records method, path, status, size, duration, request id, and the socket remote — never SQL, parameters, or secrets. Health probes are logged at debug so steady-state logs stay quiet while debug still captures everything.
func Middleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-Id")
			if !validRequestID(id) {
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

// validRequestID accepts compact, log-safe correlation ids. Anything else is replaced rather than truncated, so two attacker-controlled values cannot be made to collide by sharing a prefix.
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLength {
		return false
	}
	for idx := 0; idx < len(id); idx++ {
		c := id[idx]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == ':' || c == '/' {
			continue
		}
		return false
	}
	return true
}

func isHealthPath(p string) bool { return p == "/livez" || p == "/readyz" }

func (r *recorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	// Always forward: net/http itself ignores superfluous calls and logs its own warning, and swallowing them here would hide that handler bug.
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	// A Write without a prior WriteHeader implicitly commits the 200 the recorder starts with — a WriteHeader arriving after is superfluous and must not be recorded either.
	r.wroteHeader = true
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

// Flush forwards to the wrapped writer's Flusher. connect-go detects streaming support with a direct `w.(http.Flusher)` assertion (not http.ResponseController), so the recorder must implement Flush itself or server-streaming RPCs are rejected.
func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap exposes the wrapped writer so http.ResponseController can still reach the underlying Hijacker/deadline setters.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// remoteIP strips the port via net.SplitHostPort, which handles bracketed IPv6 ("[2001:db8::1]:443" → "2001:db8::1") — a naive last-colon split would keep the brackets or truncate a bare IPv6 address. Forwarded headers are intentionally ignored here; trusting them is decided at the proxy boundary, not in logging.
func remoteIP(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}
