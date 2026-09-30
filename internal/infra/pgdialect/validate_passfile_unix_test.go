// A build tag excludes Mkfifo from Windows compilation; a runtime skip cannot.
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

func TestValidateConnectionConfigAssemblyDoesNotBlockOnPassfile(t *testing.T) {
	// Resolve the container coordinates BEFORE polluting the environment — dbtest's own pool creation also goes through ParseConfig and would block on the FIFO in the main test goroutine, outside the select guard below.
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

		t.Fatal("config assembly blocked on the PGPASSFILE FIFO, outside any timeout")
	}
}
