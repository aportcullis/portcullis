// syscall.Mkfifo does not exist in the Windows syscall API, so this scenario
// is compiled out by build tag — a runtime GOOS skip cannot prevent the
// compile error (external review).
//go:build unix

package pgdialect_test

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/infra/pgdialect"
)

// Config assembly runs BEFORE the per-test timeout context starts, so nothing
// it touches may block: pgx's ParseConfig unconditionally OPENS the passfile
// (the password checks only gate whether the result is used — pgx v5 source),
// and a FIFO with no writer blocks os.Open forever. The passfile pin keeps
// config assembly off that file entirely (external review).
func TestValidateConnectionConfigAssemblyDoesNotBlockOnPassfile(t *testing.T) {
	// Resolve the container coordinates BEFORE polluting the environment —
	// dbtest's own pool creation also goes through ParseConfig and would block
	// on the FIFO in the main test goroutine, outside the select guard below.
	target, cred := pgCoords(t)

	fifo := filepath.Join(t.TempDir(), "pgpass.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	t.Setenv("PGPASSFILE", fifo)

	validator := pgdialect.New(pgdialect.Options{ValidateTimeout: time.Second})
	done := make(chan error, 1)
	go func() {
		done <- validator.ValidateConnection(context.Background(), target, connection.TLSModeDisable, cred)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Test with a FIFO PGPASSFILE: %v", err)
		}
	case <-time.After(5 * time.Second):
		// The goroutine stays blocked in os.Open — leaked only on failure.
		t.Fatal("config assembly blocked on the PGPASSFILE FIFO, outside any timeout")
	}
}
