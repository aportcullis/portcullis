package health_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
