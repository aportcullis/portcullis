// The browser harness owns its Testcontainers database and real application process.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "browser test server:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	listener, err := net.Listen("tcp", "127.0.0.1:18080")
	if err != nil {
		return errors.New("test server port is occupied")
	}
	if err := listener.Close(); err != nil {
		return err
	}
	targetListener, err := net.Listen("tcp", "127.0.0.1:18081")
	if err != nil {
		return errors.New("test target port is occupied")
	}
	defer func() { _ = targetListener.Close() }()
	startupCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := removeStaleBrowserContainers(startupCtx); err != nil {
		return fmt.Errorf("remove stale browser containers: %w", err)
	}
	targetImage, err := dbtest.PostgresTestImage()
	if err != nil {
		return err
	}
	databases := make([]*tcpostgres.PostgresContainer, 0, 2)
	defer func() {
		for _, database := range databases {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := database.Terminate(cleanupCtx); err != nil {
				fmt.Fprintln(os.Stderr, "browser database cleanup:", err)
			}
			cleanupCancel()
		}
	}()
	startDatabase := func(image string) (*tcpostgres.PostgresContainer, error) {
		database, err := tcpostgres.Run(startupCtx, image,
			tcpostgres.WithDatabase("portcullis"), tcpostgres.WithUsername("portcullis"), tcpostgres.WithPassword("portcullis"),
			testcontainers.WithLabels(map[string]string{browserContainerLabelKey: browserContainerLabelValue}), tcpostgres.BasicWaitStrategies())
		if err != nil {
			return nil, err
		}
		databases = append(databases, database)
		return database, nil
	}
	metadataDatabase, err := startDatabase(dbtest.PostgresImage)
	if err != nil {
		return err
	}
	targetDatabase, err := startDatabase(targetImage)
	if err != nil {
		return err
	}
	dsn, err := metadataDatabase.ConnectionString(startupCtx, "sslmode=disable")
	if err != nil {
		return err
	}
	host, err := targetDatabase.Host(startupCtx)
	if err != nil {
		return err
	}
	port, err := targetDatabase.MappedPort(startupCtx, "5432/tcp")
	if err != nil {
		return err
	}
	portNumber, err := strconv.Atoi(port.Port())
	if err != nil {
		return err
	}
	target, err := json.Marshal(map[string]any{"host": host, "port": portNumber, "database": "portcullis", "user": "portcullis", "password": "portcullis"})
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /target", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(target)
	})
	targetServer := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer func() { _ = targetServer.Close() }()
	go func() { _ = targetServer.Serve(targetListener) }()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	// Per web/e2e/login.ts, which reads the setup token from this directory outside the checkout.
	workDir := filepath.Join(os.TempDir(), "portcullis-e2e")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return err
	}
	setupTokenFile := filepath.Join(workDir, "setup-token")
	applicationLogPath := filepath.Join(workDir, "application.log")
	server := exec.Command(".test-docker/e2e/portcullis")
	server.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "PORTCULLIS_") {
			server.Env = append(server.Env, value)
		}
	}
	server.Env = append(server.Env,
		"PORTCULLIS_DATABASE_URL="+dsn, "PORTCULLIS_MASTER_KEY="+base64.StdEncoding.EncodeToString(key),
		"PORTCULLIS_ALLOW_PRIVILEGED_RUNTIME=true", "PORTCULLIS_STARTUP_MIGRATE=true",
		"PORTCULLIS_ADDR=127.0.0.1:18080", "PORTCULLIS_SHUTDOWN_TIMEOUT=5s", "PORTCULLIS_SHUTDOWN_INTERRUPT_TIMEOUT=2s",
		"PORTCULLIS_CONNECTION_ALLOWED_CIDRS="+strings.Join(dbtest.TargetDestinationCIDRs, ","),
		"PORTCULLIS_SETUP_TOKEN_FILE="+setupTokenFile)
	applicationLog, err := os.Create(applicationLogPath)
	if err != nil {
		return err
	}
	defer func() { _ = applicationLog.Close() }()
	server.Stdout, server.Stderr = io.MultiWriter(os.Stdout, applicationLog), io.MultiWriter(os.Stderr, applicationLog)
	if err := server.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- server.Wait() }()
	select {
	case err := <-exited:
		fmt.Fprintln(os.Stderr, "browser test server: application exited early; log tail follows")
		if tailErr := copyLogTail(os.Stderr, applicationLogPath, applicationLogTailBytes); tailErr != nil {
			fmt.Fprintln(os.Stderr, "browser test server: application log unavailable:", tailErr)
		}
		if err == nil {
			return errors.New("server exited before test shutdown")
		}
		return err
	case <-ctx.Done():
		_ = server.Process.Signal(syscall.SIGTERM)
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		select {
		case err := <-exited:
			return err
		case <-timer.C:
			_ = server.Process.Kill()
			<-exited
			return errors.New("server exceeded shutdown deadline")
		}
	}
}
