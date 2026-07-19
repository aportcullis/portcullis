package logging_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/platform/logging"
)

func TestPrintfLoggerRoutesToSlog(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	pl := logging.NewPrintfLogger(base, slog.LevelInfo, "testcontainers")
	pl.Printf("started container %s", "abc123")

	out := buf.String()
	if !strings.Contains(out, "started container abc123") {
		t.Errorf("message not routed to slog: %s", out)
	}
	if !strings.Contains(out, `"component":"testcontainers"`) {
		t.Errorf("component tag missing: %s", out)
	}
}

func TestNewLevel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	debug := logging.New("debug", "json")
	if !debug.Enabled(ctx, slog.LevelDebug) {
		t.Error("debug logger should have debug enabled")
	}

	info := logging.New("info", "json")
	if info.Enabled(ctx, slog.LevelDebug) {
		t.Error("info logger should not have debug enabled")
	}
}

func TestMiddlewareSetsRequestID(t *testing.T) {
	t.Parallel()
	var seen string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = logging.RequestID(r.Context())
		w.WriteHeader(http.StatusTeapot)
	})

	h := logging.Middleware(logging.New("error", "json"))(next)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if seen == "" {
		t.Error("request id not propagated to handler context")
	}
	if got := rec.Header().Get("X-Request-Id"); got == "" || got != seen {
		t.Errorf("X-Request-Id header = %q, want %q", got, seen)
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d (recorder must pass through)", rec.Code, http.StatusTeapot)
	}
}

func TestMiddlewareReusesIncomingRequestID(t *testing.T) {
	t.Parallel()
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	h := logging.Middleware(logging.New("error", "json"))(next)

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Request-Id", "abc123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-Id"); got != "abc123" {
		t.Errorf("X-Request-Id = %q, want abc123 (incoming id should be reused)", got)
	}
}

func TestMiddlewareReplacesUnsafeRequestID(t *testing.T) {
	t.Parallel()
	h := logging.Middleware(logging.New("error", "json"))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	for _, incoming := range []string{strings.Repeat("a", 129), "line break", "한글-id"} {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("X-Request-Id", incoming)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("X-Request-Id"); got == "" || got == incoming || len(got) > 128 {
			t.Errorf("unsafe X-Request-Id %q was not replaced safely; got %q", incoming, got)
		}
	}
}

func TestMiddlewareLogsFirstStatusOnDuplicateWriteHeader(t *testing.T) {
	t.Parallel()
	// net/http sends only the FIRST WriteHeader to the client and ignores the
	// rest, so the log line must record the first status too — a handler bug like
	// 401-then-500 must not log a 500 the client never received.
	cases := []struct {
		name string
		next http.HandlerFunc
		want string
	}{
		{
			name: "write header twice",
			next: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				w.WriteHeader(http.StatusInternalServerError)
			},
			want: `"status":401`,
		},
		{
			name: "write body then write header",
			next: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("ok")) // implicitly commits 200
				w.WriteHeader(http.StatusInternalServerError)
			},
			want: `"status":200`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			h := logging.Middleware(logger)(tc.next)
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
			if out := buf.String(); !strings.Contains(out, tc.want) {
				t.Errorf("log line = %s, want it to contain %s (first status wins)", out, tc.want)
			}
		})
	}
}

func TestMiddlewareKeepsResponseWriterFlushable(t *testing.T) {
	t.Parallel()
	// connect-go does a direct w.(http.Flusher) assertion for server-streaming, so
	// the logging wrapper must not hide the Flusher from the inner handler.
	var flushable bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, flushable = w.(http.Flusher)
	})
	h := logging.Middleware(logging.New("error", "json"))(next)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !flushable {
		t.Error("wrapped ResponseWriter must implement http.Flusher (Connect streaming)")
	}
}
