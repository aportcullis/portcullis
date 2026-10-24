package main_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type buildVersionBinary struct {
	path    string
	version string
}

type startupLogRecord struct {
	Level   string          `json:"level"`
	Message string          `json:"msg"`
	Version json.RawMessage `json:"version"`
}

// buildVersionExecutable compiles an independent executable with its real linker input.
func buildVersionExecutable(t *testing.T, version string) buildVersionBinary {
	t.Helper()
	projectRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(t.TempDir(), "portcullis")
	arguments := []string{"build", "-o", binaryPath}
	if version != "development" {
		arguments = append(arguments, "-ldflags=-X main.buildVersion="+version)
	}
	arguments = append(arguments, "./cmd/portcullis")
	buildContext, cancelBuild := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancelBuild()
	command := exec.CommandContext(buildContext, "go", arguments...)
	command.Dir = projectRoot
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build executable: %v\n%s", err, output)
	}
	return buildVersionBinary{path: binaryPath, version: version}
}

// buildVersionEnvironment removes inherited product settings before applying scenario inputs.
func buildVersionEnvironment(settings ...string) []string {
	environment := make([]string, 0, len(os.Environ())+len(settings))
	for _, setting := range os.Environ() {
		if !strings.HasPrefix(setting, "PORTCULLIS_") {
			environment = append(environment, setting)
		}
	}
	return append(environment, settings...)
}

// run executes one bounded entrypoint scenario and returns the actual process output.
func (binary buildVersionBinary) run(t *testing.T, arguments []string, settings ...string) ([]byte, int) {
	t.Helper()
	runContext, cancelRun := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancelRun()
	command := exec.CommandContext(runContext, binary.path, arguments...)
	command.Dir = t.TempDir()
	command.Env = buildVersionEnvironment(settings...)
	output, err := command.CombinedOutput()
	if runContext.Err() != nil {
		t.Fatalf("entrypoint exceeded its lifetime: %v\n%s", runContext.Err(), output)
	}
	if err == nil {
		return output, 0
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		return output, exitError.ExitCode()
	}
	t.Fatalf("run executable: %v", err)
	return nil, -1
}

// requireJSONStartup verifies the first configured log identifies this executable once.
func (binary buildVersionBinary) requireJSONStartup(t *testing.T, output []byte) {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(output))
	startupRecords := 0
	lineNumber := 0
	for scanner.Scan() {
		var record startupLogRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("invalid JSON log: %v\n%s", err, output)
		}
		if record.Message == "starting" {
			startupRecords++
			if lineNumber != 0 || record.Level != "INFO" || string(record.Version) != strconv.Quote(binary.version) {
				t.Fatalf("unexpected startup event: %+v at line %d; want first INFO with version %q", record, lineNumber, binary.version)
			}
		} else if len(record.Version) != 0 || bytes.Contains(scanner.Bytes(), []byte(binary.version)) {
			t.Fatalf("version field escaped the startup event: %s", scanner.Bytes())
		}
		lineNumber++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if startupRecords != 1 {
		t.Fatalf("startup events = %d, want one INFO version %q\n%s", startupRecords, binary.version, output)
	}
}
