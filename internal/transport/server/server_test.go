package server_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	"github.com/aportcullis/portcullis/internal/transport/connectapi"
	"github.com/aportcullis/portcullis/internal/transport/server"
)

// newTestServer builds the full HTTP surface (health, SPA, Connect RPC) for
// in-process e2e tests.
func newTestServer(t *testing.T) (*server.Server, *httptest.Server) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	healthPath, healthHandler := portcullisv1connect.NewHealthHandler(connectapi.HealthService{})
	srv := server.New(":0", logger, 0, server.Mount{Pattern: healthPath, Handler: healthHandler})

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func get(t *testing.T, base, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func TestE2E_Liveness(t *testing.T) {
	t.Parallel()
	_, ts := newTestServer(t)
	resp := get(t, ts.URL, "/livez")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/livez = %d, want 200", resp.StatusCode)
	}
}

func TestE2E_Readiness(t *testing.T) {
	t.Parallel()
	srv, ts := newTestServer(t)

	resp := get(t, ts.URL, "/readyz")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/readyz = %d, want 200 with no checks", resp.StatusCode)
	}

	// A failing dependency makes the service unready (but stays alive).
	srv.Health().Register("db", func(context.Context) error { return errors.New("down") })
	resp = get(t, ts.URL, "/readyz")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("/readyz with failing check = %d, want 503", resp.StatusCode)
	}

	resp = get(t, ts.URL, "/livez")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/livez with failing dependency = %d, want 200 (liveness ignores deps)", resp.StatusCode)
	}
}

func TestE2E_ConnectHealth(t *testing.T) {
	t.Parallel()
	_, ts := newTestServer(t)

	client := portcullisv1connect.NewHealthClient(http.DefaultClient, ts.URL)
	resp, err := client.Check(context.Background(), connect.NewRequest(&portcullisv1.CheckRequest{}))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got := resp.Msg.GetStatus(); got != "ok" {
		t.Errorf("status = %q, want ok", got)
	}
}

func TestE2E_SPAPlaceholder(t *testing.T) {
	t.Parallel()
	_, ts := newTestServer(t)
	resp := get(t, ts.URL, "/")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/ = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Portcullis") {
		t.Errorf("/ body missing app name; got %q", string(body))
	}
}

// The drain delay and the shutdown timeout are SEQUENTIAL budgets (ADR-0010):
// the delay must elapse in full — so Kubernetes deregisters the pod — even when
// it exceeds the shutdown timeout, and Shutdown must still succeed instead of
// handing http.Server.Shutdown an already-expired context.
func TestShutdownDrainDelayNotChargedToShutdownTimeout(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	const drain = 80 * time.Millisecond
	s := server.New("127.0.0.1:0", logger, drain)

	start := time.Now()
	if err := s.Shutdown(context.Background(), 20*time.Millisecond); err != nil {
		t.Fatalf("Shutdown = %v; the drain delay must not consume the shutdown budget", err)
	}
	if elapsed := time.Since(start); elapsed < drain {
		t.Errorf("drain delay cut short: elapsed %v, want >= %v", elapsed, drain)
	}
}
