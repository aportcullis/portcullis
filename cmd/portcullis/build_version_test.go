package main_test

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
)

func TestBuildVersionBinary(t *testing.T) {
	development := buildVersionExecutable(t, "development")
	stable := buildVersionExecutable(t, "v0.1.0")
	prerelease := buildVersionExecutable(t, "v7.8.9-qa.37")
	masterKey := "PORTCULLIS_MASTER_KEY=" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x37}, 32))

	t.Run("missing-key-still-identifies-linked-executable", func(t *testing.T) {
		output, exitCode := stable.run(t, nil, "PORTCULLIS_LOG_FORMAT=json")
		requireEntrypointRefusal(t, output, exitCode, 1, "master key required")
		stable.requireJSONStartup(t, output)
	})

	t.Run("malformed-key-refuses-without-echoing-key", func(t *testing.T) {
		const privateKey = "invalid-key-private-sentinel"
		output, exitCode := stable.run(t, nil, "PORTCULLIS_MASTER_KEY="+privateKey)
		requireEntrypointRefusal(t, output, exitCode, 1, "master key required")
		stable.requireJSONStartup(t, output)
		if bytes.Contains(output, []byte(privateKey)) {
			t.Fatalf("malformed key was logged: %s", output)
		}
	})

	t.Run("missing-database-refuses-after-startup-event", func(t *testing.T) {
		output, exitCode := stable.run(t, nil, masterKey)
		requireEntrypointRefusal(t, output, exitCode, 1, "database required")
		stable.requireJSONStartup(t, output)
	})

	t.Run("malformed-database-preserves-credential-privacy", func(t *testing.T) {
		const privatePassword = "database-password-private-sentinel"
		const malformedDSN = "postgres://fixture:" + privatePassword + "@127.0.0.1:not-a-port/metadata"
		output, exitCode := stable.run(t, nil, masterKey, "PORTCULLIS_DATABASE_URL="+malformedDSN)
		requireEntrypointRefusal(t, output, exitCode, 1, "database config invalid")
		stable.requireJSONStartup(t, output)
		if bytes.Contains(output, []byte(privatePassword)) || bytes.Contains(output, []byte(malformedDSN)) {
			t.Fatalf("database credentials were logged: %s", output)
		}
	})

	for _, setting := range []string{"PORTCULLIS_LOG_FORMAT=invalid", "PORTCULLIS_LOG_LEVEL=invalid"} {
		t.Run("invalid-logging-"+setting, func(t *testing.T) {
			output, exitCode := stable.run(t, nil, setting)
			requireEntrypointRefusal(t, output, exitCode, 1, "config load failed")
			requireNoStartupIdentity(t, output, stable.version)
		})
	}

	for _, level := range []string{"warn", "error"} {
		t.Run("normal-"+level+"-filter-hides-info", func(t *testing.T) {
			output, exitCode := stable.run(t, nil, masterKey, "PORTCULLIS_LOG_LEVEL="+level)
			requireEntrypointRefusal(t, output, exitCode, 1, "database required")
			requireNoStartupIdentity(t, output, stable.version)
			if bytes.Contains(output, []byte(`"level":"INFO"`)) {
				t.Fatalf("INFO escaped the configured level: %s", output)
			}
		})
	}

	t.Run("runtime-version-settings-cannot-replace-linked-identity", func(t *testing.T) {
		const runtimeVersion = "v99.99.99-runtime-spoof"
		output, exitCode := prerelease.run(t, []string{"serve"}, "BUILD_VERSION="+runtimeVersion,
			"PORTCULLIS_BUILD_VERSION="+runtimeVersion, "PORTCULLIS_VERSION="+runtimeVersion)
		requireEntrypointRefusal(t, output, exitCode, 1, "master key required")
		prerelease.requireJSONStartup(t, output)
		if bytes.Contains(output, []byte(runtimeVersion)) {
			t.Fatalf("runtime settings replaced the linked identity: %s", output)
		}
	})

	for _, scenario := range []struct {
		name      string
		arguments []string
		exitCode  int
		message   string
	}{
		{name: "unknown-command", arguments: []string{"not-a-command"}, exitCode: 2, message: "unknown command"},
		{name: "migration-is-not-server-startup", arguments: []string{"migrate"}, exitCode: 1, message: "database required"},
		{name: "key-rotation-is-not-server-startup", arguments: []string{"key", "rotate"}, exitCode: 1, message: "rotation requires historical and new active keys"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			output, exitCode := stable.run(t, scenario.arguments, masterKey)
			requireEntrypointRefusal(t, output, exitCode, scenario.exitCode, scenario.message)
			requireNoStartupIdentity(t, output, stable.version)
		})
	}

	t.Run("healthy-executables-keep-health-surfaces-version-free", func(t *testing.T) {
		pool := dbtest.FreshPostgres(t)
		if err := postgres.Migrate(t.Context(), pool); err != nil {
			t.Fatalf("migrate metadata fixture: %v", err)
		}
		for _, scenario := range []struct {
			name      string
			binary    buildVersionBinary
			arguments []string
			format    string
		}{
			{name: "default-json", binary: development, format: "json"},
			{name: "stable-serve-json", binary: stable, arguments: []string{"serve"}, format: "json"},
			{name: "prerelease-json-default-command", binary: prerelease, format: "json"},
			{name: "prerelease-serve-text", binary: prerelease, arguments: []string{"serve"}, format: "text"},
		} {
			t.Run(scenario.name, func(t *testing.T) {
				process := scenario.binary.start(t, scenario.arguments, masterKey,
					"PORTCULLIS_DATABASE_URL="+pool.Config().ConnString(),
					"PORTCULLIS_ALLOW_PRIVILEGED_RUNTIME=true", "PORTCULLIS_LOG_FORMAT="+scenario.format)
				process.requireHealthyResponses(t)
				output := process.stop(t)
				if scenario.format == "json" {
					scenario.binary.requireJSONStartup(t, output)
				} else {
					scenario.binary.requireTextStartup(t, output)
				}
			})
		}
	})
}

// requireEntrypointRefusal checks the command's real failure status and public diagnostic.
func requireEntrypointRefusal(t *testing.T, output []byte, exitCode, expectedExit int, message string) {
	t.Helper()
	if exitCode != expectedExit || !bytes.Contains(output, []byte(message)) {
		t.Fatalf("entrypoint exit = %d, want %d with %q\n%s", exitCode, expectedExit, message, output)
	}
}

// requireNoStartupIdentity keeps pre-logger and non-serving commands version-free.
func requireNoStartupIdentity(t *testing.T, output []byte, version string) {
	t.Helper()
	if bytes.Contains(output, []byte(`"msg":"starting"`)) || bytes.Contains(output, []byte(version)) ||
		strings.Contains(string(output), `"version":`) {
		t.Fatalf("non-startup output exposed the server identity: %s", output)
	}
}
