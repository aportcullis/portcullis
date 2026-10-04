package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyLogTailShowsTheEndOfTheApplicationLog(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		content  string
		maxBytes int64
		want     string
	}{
		{name: "short startup log is copied whole", content: "crypto ready\nmetadata db unreachable\n", maxBytes: 1024, want: "crypto ready\nmetadata db unreachable\n"},
		{name: "log exactly at the bound is copied whole", content: "0123456789", maxBytes: 10, want: "0123456789"},
		{name: "long log keeps only its last bytes", content: strings.Repeat("request\n", 100) + "fatal: listen failed\n", maxBytes: 21, want: "fatal: listen failed\n"},
		{name: "empty log copies nothing", content: "", maxBytes: 64, want: ""},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "application.log")
			if err := os.WriteFile(path, []byte(scenario.content), 0o600); err != nil {
				t.Fatal(err)
			}
			var copied bytes.Buffer
			if err := copyLogTail(&copied, path, scenario.maxBytes); err != nil {
				t.Fatalf("copyLogTail: %v", err)
			}
			if copied.String() != scenario.want {
				t.Fatalf("copied %q, want %q", copied.String(), scenario.want)
			}
		})
	}
}

func TestCopyLogTailRefusesUnreadableLogs(t *testing.T) {
	directory := t.TempDir()
	written := filepath.Join(directory, "application.log")
	if err := os.WriteFile(written, []byte("crypto ready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name     string
		path     string
		maxBytes int64
	}{
		{name: "missing log file", path: filepath.Join(directory, "never-written.log"), maxBytes: 64},
		{name: "directory instead of a log file", path: directory, maxBytes: 64},
		{name: "zero byte bound", path: written, maxBytes: 0},
		{name: "negative byte bound", path: written, maxBytes: -1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var copied bytes.Buffer
			if err := copyLogTail(&copied, scenario.path, scenario.maxBytes); err == nil {
				t.Fatalf("copyLogTail(%q, %d) succeeded, want an error", scenario.path, scenario.maxBytes)
			}
			if copied.Len() != 0 {
				t.Fatalf("refused copy still wrote %q", copied.String())
			}
		})
	}
}
