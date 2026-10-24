package main_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
)

type buildVersionProcess struct {
	binary    buildVersionBinary
	command   *exec.Cmd
	baseURL   string
	output    bytes.Buffer
	done      chan struct{}
	waitError error
	cancel    context.CancelFunc
}

// start owns a bounded child process and its isolated setup-token destination.
func (binary buildVersionBinary) start(t *testing.T, arguments []string, settings ...string) *buildVersionProcess {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	workingDirectory := t.TempDir()
	settings = append(settings, "PORTCULLIS_ADDR="+address,
		"PORTCULLIS_SETUP_TOKEN_FILE="+filepath.Join(workingDirectory, "setup-token"),
		"PORTCULLIS_SHUTDOWN_TIMEOUT=3s", "PORTCULLIS_SHUTDOWN_INTERRUPT_TIMEOUT=1s")
	runContext, cancelRun := context.WithTimeout(t.Context(), 45*time.Second)
	command := exec.CommandContext(runContext, binary.path, arguments...)
	command.Dir = workingDirectory
	command.Env = buildVersionEnvironment(settings...)
	process := &buildVersionProcess{binary: binary, command: command, baseURL: "http://" + address,
		done: make(chan struct{}), cancel: cancelRun}
	command.Stdout = &process.output
	command.Stderr = &process.output
	if err := command.Start(); err != nil {
		cancelRun()
		t.Fatalf("start executable: %v", err)
	}
	go func() {
		process.waitError = command.Wait()
		close(process.done)
	}()
	t.Cleanup(func() { process.stop(t) })
	return process
}

// stop waits for graceful child cleanup and reports any abnormal process exit.
func (process *buildVersionProcess) stop(t *testing.T) []byte {
	t.Helper()
	select {
	case <-process.done:
	default:
		if err := process.command.Process.Signal(syscall.SIGTERM); err != nil {
			t.Errorf("signal executable: %v", err)
		}
		select {
		case <-process.done:
		case <-time.After(5 * time.Second):
			if err := process.command.Process.Kill(); err != nil {
				t.Errorf("kill overdue executable: %v", err)
			}
			<-process.done
			t.Error("executable did not stop within its cleanup budget")
		}
	}
	process.cancel()
	if process.waitError != nil {
		t.Errorf("executable exit: %v\n%s", process.waitError, process.output.Bytes())
	}
	return process.output.Bytes()
}

// requireHealthyResponses inspects raw public response fields and headers after real readiness.
func (process *buildVersionProcess) requireHealthyResponses(t *testing.T) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	readyContext, cancelReady := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancelReady()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, err := http.NewRequestWithContext(readyContext, http.MethodGet, process.baseURL+"/readyz", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err == nil {
			if err := response.Body.Close(); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		select {
		case <-process.done:
			t.Fatalf("executable stopped before readiness\n%s", process.output.Bytes())
		case <-readyContext.Done():
			t.Fatalf("executable did not become ready: %v\n%s", readyContext.Err(), process.stop(t))
		case <-ticker.C:
		}
	}
	for _, path := range []string{"/livez", "/readyz", portcullisv1connect.HealthCheckProcedure} {
		method := http.MethodGet
		var body io.Reader
		if path == portcullisv1connect.HealthCheckProcedure {
			method = http.MethodPost
			body = strings.NewReader("{}")
		}
		request, err := http.NewRequestWithContext(t.Context(), method, process.baseURL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		if method == http.MethodPost {
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Connect-Protocol-Version", "1")
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("request %s: %v", path, err)
		}
		process.requireVersionFreeHealth(t, path, response)
	}
}

// requireVersionFreeHealth rejects extra response fields and build-identifying headers.
func (process *buildVersionProcess) requireVersionFreeHealth(t *testing.T, path string, response *http.Response) {
	t.Helper()
	body, readError := io.ReadAll(io.LimitReader(response.Body, 4097))
	closeError := response.Body.Close()
	if readError != nil || closeError != nil {
		t.Fatalf("read %s: read=%v close=%v", path, readError, closeError)
	}
	if response.StatusCode != http.StatusOK || len(body) > 4096 {
		t.Fatalf("health %s returned %d: %s", path, response.StatusCode, body)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("decode raw %s: %v", path, err)
	}
	expectedFields := 1
	if path == "/readyz" {
		expectedFields = 2
		var checks map[string]string
		if err := json.Unmarshal(fields["checks"], &checks); err != nil || len(checks) != 1 || checks["metadata-db"] != "ok" {
			t.Fatalf("readiness checks changed: %s", body)
		}
	}
	if len(fields) != expectedFields || string(fields["status"]) != `"ok"` {
		t.Fatalf("public health fields changed at %s: %s", path, body)
	}
	if bytes.Contains(body, []byte(process.binary.version)) {
		t.Fatalf("health %s exposed the injected version: %s", path, body)
	}
	for header, values := range response.Header {
		headerName := strings.ToLower(header)
		if headerName == "server" || strings.Contains(headerName, "build") ||
			(strings.Contains(headerName, "version") && headerName != "connect-protocol-version") ||
			strings.Contains(strings.Join(values, " "), process.binary.version) {
			t.Fatalf("health %s exposed build metadata through header %s: %q", path, header, values)
		}
	}
}

// requireTextStartup checks the text handler preserves the exact linked version once.
func (binary buildVersionBinary) requireTextStartup(t *testing.T, output []byte) {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(output))
	startupRecords := 0
	lineNumber := 0
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if slices.Contains(fields, "msg=starting") {
			startupRecords++
			if lineNumber != 0 || !slices.Contains(fields, "level=INFO") || !slices.Contains(fields, "version="+binary.version) {
				t.Fatalf("unexpected text startup event: %s", line)
			}
		} else if strings.Contains(line, " version=") || strings.Contains(line, binary.version) {
			t.Fatalf("version escaped the text startup event: %s", line)
		}
		lineNumber++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if startupRecords != 1 {
		t.Fatalf("text startup events = %d, want one version %q\n%s", startupRecords, binary.version, output)
	}
}
