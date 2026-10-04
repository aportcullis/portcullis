package server_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
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

func newTestServer(t *testing.T) (*server.Server, *httptest.Server) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	healthPath, healthHandler := portcullisv1connect.NewHealthHandler(connectapi.HealthService{})
	srv, err := server.New(server.Options{Addr: ":0", Logger: logger, Hosts: loopbackHostPolicy(t)}, server.Mount{Pattern: healthPath, Handler: healthHandler})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

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

func TestE2E_SecurityHeaders(t *testing.T) {
	t.Parallel()
	_, ts := newTestServer(t)
	resp := get(t, ts.URL, "/")
	defer func() { _ = resp.Body.Close() }()

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	}
	for k, v := range want {
		if got := resp.Header.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if resp.Header.Get("Content-Security-Policy") == "" {
		t.Error("missing Content-Security-Policy header")
	}
	if resp.Header.Get("Referrer-Policy") == "" {
		t.Error("missing Referrer-Policy header")
	}
	if got := resp.Header.Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS = %q, want empty (owned by the TLS-terminating proxy)", got)
	}
}

// requestProbe is a handler that reports when a request starts and how its context ended.
type requestProbe struct {
	started  chan struct{}
	finish   chan struct{}
	ended    chan error
	hookSeen chan error
}

func newRequestProbe() *requestProbe {
	return &requestProbe{started: make(chan struct{}, 1), finish: make(chan struct{}), ended: make(chan error, 1), hookSeen: make(chan error, 1)}
}

func (p *requestProbe) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	p.started <- struct{}{}
	select {
	case <-request.Context().Done():
		p.ended <- request.Context().Err()
	case <-p.finish:
		p.ended <- nil
		writer.WriteHeader(http.StatusNoContent)
	}
}

// serveProbe starts a listening server with the probe mounted at /probe.
func serveProbe(t *testing.T, probe *requestProbe) (*server.Server, string) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := server.New(server.Options{Addr: "127.0.0.1:0", Logger: logger, Hosts: loopbackHostPolicy(t)}, server.Mount{Pattern: "/probe", Handler: probe})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(listener) }()
	return srv, "http://" + listener.Addr().String()
}

// startProbeRequest issues one request in the background and waits until the handler runs.
func startProbeRequest(t *testing.T, probe *requestProbe, base string) {
	t.Helper()
	go func() {
		response, err := http.Get(base + "/probe")
		if err == nil {
			_ = response.Body.Close()
		}
	}()
	select {
	case <-probe.started:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not reach the handler")
	}
}

func TestShutdownTimeoutRunsHooksThenCancelsInFlightRequests(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name         string
		inFlight     bool
		finishInTime bool
		wantErr      bool
		wantHook     bool
	}{
		{"idle server", false, false, false, false},
		{"request finishing within the timeout", true, true, false, false},
		{"request outliving the timeout", true, false, true, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			probe := newRequestProbe()
			srv, base := serveProbe(t, probe)
			hookRuns := 0
			srv.OnShutdownTimeout(func(ctx context.Context) {
				hookRuns++
				if ctx.Err() != nil {
					probe.hookSeen <- ctx.Err()
					return
				}
				select {
				case err := <-probe.ended:
					probe.hookSeen <- errors.Join(errors.New("request context ended before the hook ran"), err)
				default:
					probe.hookSeen <- nil
				}
			})
			if scenario.inFlight {
				startProbeRequest(t, probe, base)
			}
			if scenario.finishInTime {
				time.AfterFunc(20*time.Millisecond, func() { close(probe.finish) })
			}
			err := srv.Shutdown(context.Background(), 200*time.Millisecond)
			if (err != nil) != scenario.wantErr {
				t.Fatalf("Shutdown = %v, want error %v", err, scenario.wantErr)
			}
			if (hookRuns == 1) != scenario.wantHook || hookRuns > 1 {
				t.Fatalf("hook runs = %d, want hook %v", hookRuns, scenario.wantHook)
			}
			if scenario.wantHook {
				if hookErr := <-probe.hookSeen; hookErr != nil {
					t.Fatalf("hook ran with %v", hookErr)
				}
			}
			if !scenario.inFlight {
				return
			}
			select {
			case ended := <-probe.ended:
				if scenario.finishInTime && ended != nil {
					t.Fatalf("finished request was cancelled: %v", ended)
				}
				if !scenario.finishInTime && !errors.Is(ended, context.Canceled) {
					t.Fatalf("stuck request ended with %v, want context.Canceled", ended)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("stuck request context was never cancelled")
			}
		})
	}
}

func TestShutdownTimeoutHooksRunInRegistrationOrderOnce(t *testing.T) {
	t.Parallel()
	probe := newRequestProbe()
	srv, base := serveProbe(t, probe)
	var order []string
	srv.OnShutdownTimeout(func(context.Context) { order = append(order, "executions") })
	srv.OnShutdownTimeout(func(context.Context) { order = append(order, "streams") })
	startProbeRequest(t, probe, base)
	if err := srv.Shutdown(context.Background(), 50*time.Millisecond); err == nil {
		t.Fatal("stuck request did not fail the shutdown")
	}
	if strings.Join(order, ",") != "executions,streams" {
		t.Fatalf("hook order = %v", order)
	}
}

// parseCSPDirectives splits a Content-Security-Policy header into directive name → source list.
func parseCSPDirectives(header string) map[string][]string {
	directives := map[string][]string{}
	for _, directive := range strings.Split(header, ";") {
		fields := strings.Fields(directive)
		if len(fields) > 0 {
			directives[fields[0]] = fields[1:]
		}
	}
	return directives
}

func TestContentSecurityPolicyRestrictsEveryFetchToTheSPAOrigin(t *testing.T) {
	t.Parallel()
	_, ts := newTestServer(t)
	for _, path := range []string{"/", "/login", "/livez", "/assets/missing.js"} {
		resp := get(t, ts.URL, path)
		_ = resp.Body.Close()
		directives := parseCSPDirectives(resp.Header.Get("Content-Security-Policy"))
		// Success: each fetch class the SPA uses is limited to its own origin, with data: images and inline styles kept for the embedded bundle.
		want := map[string]string{
			"default-src":     "'self'",
			"script-src":      "'self'",
			"style-src":       "'self' 'unsafe-inline'",
			"img-src":         "'self' data:",
			"connect-src":     "'self'",
			"object-src":      "'none'",
			"frame-ancestors": "'none'",
			"base-uri":        "'self'",
			"form-action":     "'self'",
		}
		for name, sources := range want {
			if got := strings.Join(directives[name], " "); got != sources {
				t.Errorf("%s: %s = %q, want %q", path, name, got, sources)
			}
		}
		// Refusal: no directive may admit inline or evaluated script, wildcard or scheme-wide origins, or blob: sources.
		for name, sources := range directives {
			for _, source := range sources {
				switch {
				case name != "style-src" && source == "'unsafe-inline'",
					source == "'unsafe-eval'", source == "*", source == "https:", source == "http:", source == "blob:":
					t.Errorf("%s: %s admits %s", path, name, source)
				}
			}
		}
	}
}

func TestShutdownDrainDelayNotChargedToShutdownTimeout(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	const drain = 80 * time.Millisecond
	s, err := server.New(server.Options{Addr: "127.0.0.1:0", Logger: logger, DrainDelay: drain, Hosts: loopbackHostPolicy(t)})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	start := time.Now()
	if err := s.Shutdown(context.Background(), 20*time.Millisecond); err != nil {
		t.Fatalf("Shutdown = %v; the drain delay must not consume the shutdown budget", err)
	}
	if elapsed := time.Since(start); elapsed < drain {
		t.Errorf("drain delay cut short: elapsed %v, want >= %v", elapsed, drain)
	}
}
