package health_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/platform/health"
)

func TestLiveAlwaysOK(t *testing.T) {
	t.Parallel()
	h := health.New()
	rec := httptest.NewRecorder()
	h.Live(rec, httptest.NewRequest(http.MethodGet, "/livez", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("Live status = %d, want 200", rec.Code)
	}
}

func TestReadyNoChecks(t *testing.T) {
	t.Parallel()
	h := health.New()
	rec := httptest.NewRecorder()
	h.Ready(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("Ready status = %d, want 200", rec.Code)
	}
}

func TestReadyFailingCheck(t *testing.T) {
	t.Parallel()
	h := health.New()
	h.Register("db", func(context.Context) error { return errors.New("unreachable") })

	rec := httptest.NewRecorder()
	h.Ready(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Ready status = %d, want 503", rec.Code)
	}
}

func TestReadyDraining(t *testing.T) {
	t.Parallel()
	h := health.New()
	h.StartDraining()

	rec := httptest.NewRecorder()
	h.Ready(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Ready status = %d, want 503 while draining", rec.Code)
	}
}

func TestReadyDoesNotLeakCheckError(t *testing.T) {
	t.Parallel()
	var logBuf bytes.Buffer
	h := health.New().WithLogger(slog.New(slog.NewTextHandler(&logBuf, nil)))
	h.Register("db", func(context.Context) error {
		return errors.New("dial tcp: password=topsecret host=internal-db")
	})

	rec := httptest.NewRecorder()
	h.Ready(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "topsecret") || strings.Contains(body, "internal-db") {
		t.Errorf("readiness body leaked the check error: %s", body)
	}
	if !strings.Contains(body, "unavailable") {
		t.Errorf("readiness body should mark the failing check unavailable: %s", body)
	}
	// The server-side log must not carry the secret either — only the error type.
	if logged := logBuf.String(); strings.Contains(logged, "topsecret") || strings.Contains(logged, "internal-db") {
		t.Errorf("log leaked the check error detail: %s", logged)
	}
}
