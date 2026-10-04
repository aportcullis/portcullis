package main

// White-box test: package main cannot be imported by an external test package, and setup-token delivery is composition-root behavior with no other exported surface.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

const deliveredToken = "issued-setup-token-value"

type fixedSetupTokenIssuer struct {
	token string
	err   error
}

func (issuer fixedSetupTokenIssuer) IssueSetupToken(context.Context) (string, error) {
	return issuer.token, issuer.err
}

func newCapturingLogger() (*slog.Logger, *bytes.Buffer) {
	buffer := &bytes.Buffer{}
	return slog.New(slog.NewTextHandler(buffer, nil)), buffer
}

func readTokenFile(t *testing.T, path string) (string, os.FileMode) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	return string(content), info.Mode().Perm()
}

func TestDeliverFirstRunSetupTokenHandsTheTokenOverOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("configured file receives the token with owner-only mode", func(t *testing.T) {
		t.Parallel()
		logger, logs := newCapturingLogger()
		path := filepath.Join(t.TempDir(), "setup-token")
		if err := deliverFirstRunSetupToken(ctx, logger, fixedSetupTokenIssuer{token: deliveredToken}, path); err != nil {
			t.Fatalf("deliverFirstRunSetupToken: %v", err)
		}
		content, mode := readTokenFile(t, path)
		if content != deliveredToken+"\n" || mode != 0o600 {
			t.Errorf("token file = %q mode %o, want the token and 0600", content, mode)
		}
		if strings.Contains(logs.String(), deliveredToken) || !strings.Contains(logs.String(), path) {
			t.Errorf("file delivery log must name the path and never the token: %s", logs.String())
		}
	})

	t.Run("existing broad file is replaced and narrowed", func(t *testing.T) {
		t.Parallel()
		logger, _ := newCapturingLogger()
		path := filepath.Join(t.TempDir(), "setup-token")
		if err := os.WriteFile(path, []byte("previous-start-token-that-is-longer\n"), 0o644); err != nil {
			t.Fatalf("seed token file: %v", err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatalf("chmod seed: %v", err)
		}
		if err := deliverFirstRunSetupToken(ctx, logger, fixedSetupTokenIssuer{token: deliveredToken}, path); err != nil {
			t.Fatalf("deliverFirstRunSetupToken: %v", err)
		}
		if content, mode := readTokenFile(t, path); content != deliveredToken+"\n" || mode != 0o600 {
			t.Errorf("token file = %q mode %o, want only the new token and 0600", content, mode)
		}
	})

	t.Run("symlink at the path is replaced without writing through it", func(t *testing.T) {
		t.Parallel()
		logger, _ := newCapturingLogger()
		directory := t.TempDir()
		victim := filepath.Join(directory, "victim")
		if err := os.WriteFile(victim, []byte("unrelated file\n"), 0o644); err != nil {
			t.Fatalf("seed victim: %v", err)
		}
		path := filepath.Join(directory, "setup-token")
		if err := os.Symlink(victim, path); err != nil {
			t.Fatalf("plant symlink: %v", err)
		}
		if err := deliverFirstRunSetupToken(ctx, logger, fixedSetupTokenIssuer{token: deliveredToken}, path); err != nil {
			t.Fatalf("deliverFirstRunSetupToken: %v", err)
		}
		if victimContent, err := os.ReadFile(victim); err != nil || string(victimContent) != "unrelated file\n" {
			t.Errorf("symlink target = %q (%v), want it untouched", victimContent, err)
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("token path is not a regular file after delivery (%v, %v)", info, err)
		}
		if content, mode := readTokenFile(t, path); content != deliveredToken+"\n" || mode != 0o600 {
			t.Errorf("token file = %q mode %o, want the token and 0600", content, mode)
		}
	})

	t.Run("repeated starts leave only the latest token and no temporary files", func(t *testing.T) {
		t.Parallel()
		logger, _ := newCapturingLogger()
		directory := t.TempDir()
		path := filepath.Join(directory, "setup-token")
		for _, token := range []string{"first-start-token", "second-start-token", deliveredToken} {
			if err := deliverFirstRunSetupToken(ctx, logger, fixedSetupTokenIssuer{token: token}, path); err != nil {
				t.Fatalf("deliverFirstRunSetupToken(%q): %v", token, err)
			}
		}
		if content, _ := readTokenFile(t, path); content != deliveredToken+"\n" {
			t.Errorf("token file = %q, want only the latest token", content)
		}
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 1 {
			t.Errorf("directory holds %d entries (%v), want only the token file", len(entries), err)
		}
	})

	t.Run("without a file the token is logged once", func(t *testing.T) {
		t.Parallel()
		logger, logs := newCapturingLogger()
		if err := deliverFirstRunSetupToken(ctx, logger, fixedSetupTokenIssuer{token: deliveredToken}, ""); err != nil {
			t.Fatalf("deliverFirstRunSetupToken: %v", err)
		}
		if occurrences := strings.Count(logs.String(), deliveredToken); occurrences != 1 {
			t.Errorf("token appears %d times in the log, want exactly 1: %s", occurrences, logs.String())
		}
	})

	t.Run("bootstrapped installation issues and logs nothing", func(t *testing.T) {
		t.Parallel()
		logger, logs := newCapturingLogger()
		path := filepath.Join(t.TempDir(), "setup-token")
		if err := deliverFirstRunSetupToken(ctx, logger, fixedSetupTokenIssuer{err: identity.ErrAlreadyBootstrapped}, path); err != nil {
			t.Fatalf("deliverFirstRunSetupToken = %v, want nil once bootstrapped", err)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("bootstrapped installation wrote a token file (stat err %v)", err)
		}
		if logs.Len() != 0 {
			t.Errorf("bootstrapped installation logged: %s", logs.String())
		}
	})
}

func TestDeliverFirstRunSetupTokenFailsStartupWhenDeliveryFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	failures := []struct {
		name   string
		issuer fixedSetupTokenIssuer
		path   func(t *testing.T) string
	}{
		{name: "missing directory", issuer: fixedSetupTokenIssuer{token: deliveredToken}, path: func(t *testing.T) string {
			return filepath.Join(t.TempDir(), "absent", "setup-token")
		}},
		{name: "path is a directory", issuer: fixedSetupTokenIssuer{token: deliveredToken}, path: func(t *testing.T) string {
			directory := t.TempDir()
			path := filepath.Join(directory, "setup-token")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatalf("create directory at token path: %v", err)
			}
			// The rename onto the directory fails after the temporary file exists, so its removal is observable here.
			t.Cleanup(func() {
				if entries, err := os.ReadDir(directory); err != nil || len(entries) != 1 {
					t.Errorf("failed delivery left %d entries (%v), want only the directory", len(entries), err)
				}
			})
			return path
		}},
		{name: "read-only directory", issuer: fixedSetupTokenIssuer{token: deliveredToken}, path: func(t *testing.T) string {
			directory := t.TempDir()
			if err := os.Chmod(directory, 0o500); err != nil {
				t.Fatalf("chmod directory: %v", err)
			}
			t.Cleanup(func() { _ = os.Chmod(directory, 0o700) })
			return filepath.Join(directory, "setup-token")
		}},
		{name: "issuance outage", issuer: fixedSetupTokenIssuer{err: errors.New("database unavailable")}, path: func(*testing.T) string { return "" }},
	}
	for _, failure := range failures {
		t.Run(failure.name, func(t *testing.T) {
			t.Parallel()
			logger, logs := newCapturingLogger()
			if err := deliverFirstRunSetupToken(ctx, logger, failure.issuer, failure.path(t)); err == nil {
				t.Fatal("deliverFirstRunSetupToken succeeded, want a startup failure")
			}
			if strings.Contains(logs.String(), deliveredToken) {
				t.Errorf("failed delivery leaked the token to the log: %s", logs.String())
			}
		})
	}
}
