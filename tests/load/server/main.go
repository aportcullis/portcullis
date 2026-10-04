// The load harness owns disposable databases, synthetic identities and the real application process.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/aportcullis/portcullis/tests/load/server/binaryidentity"
	"github.com/aportcullis/portcullis/tests/load/server/loadconfig"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "load fixture failed:", err)
		os.Exit(1)
	}
}

// run prepares an isolated least-privilege installation and removes its resources at shutdown.
func run() error {
	settings, err := loadconfig.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if _, err := os.Stat("tests/load/fixtures.local.json"); err == nil {
		return errors.New("private load fixture already exists; move it before startup")
	} else if !os.IsNotExist(err) {
		return err
	}
	startup, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:18082")
	if err != nil {
		return errors.New("load server port occupied")
	}
	_ = listener.Close()
	databases := make([]*tcpostgres.PostgresContainer, 0, 2)
	defer func() {
		for _, database := range databases {
			cleanup, finish := context.WithTimeout(context.Background(), 15*time.Second)
			_ = database.Terminate(cleanup)
			finish()
		}
	}()
	startDatabase := func(role string) (*tcpostgres.PostgresContainer, error) {
		database, err := tcpostgres.Run(startup, dbtest.PostgresImage, tcpostgres.WithDatabase("portcullis"), tcpostgres.WithUsername("owner"), tcpostgres.WithPassword("synthetic-owner-password"), testcontainers.WithLabels(map[string]string{"portcullis.test": "load", "portcullis.database": role}), tcpostgres.BasicWaitStrategies())
		if err != nil {
			return nil, errors.New("cannot start disposable database")
		}
		databases = append(databases, database)
		return database, nil
	}
	metadataDatabase, err := startDatabase("metadata")
	if err != nil {
		return err
	}
	targetDatabase, err := startDatabase("target")
	if err != nil {
		return err
	}
	metadataDSN, err := metadataDatabase.ConnectionString(startup, "sslmode=disable")
	if err != nil {
		return err
	}
	pool, err := pgxpool.New(startup, metadataDSN)
	if err != nil {
		return errors.New("cannot connect metadata fixture")
	}
	defer pool.Close()
	if err := postgres.Migrate(startup, pool); err != nil {
		return err
	}
	if _, err := pool.Exec(startup, "create role load_app login password 'synthetic-runtime-password'; grant portcullis_runtime to load_app"); err != nil {
		return err
	}
	runtimeURL, err := url.Parse(metadataDSN)
	if err != nil {
		return err
	}
	runtimeURL.User = url.UserPassword("load_app", "synthetic-runtime-password")
	targetDSN, err := targetDatabase.ConnectionString(startup, "sslmode=disable")
	if err != nil {
		return err
	}
	targetPool, err := pgxpool.New(startup, targetDSN)
	if err != nil {
		return err
	}
	defer targetPool.Close()
	if _, err := targetPool.Exec(startup, "create table load_numbers (id bigint primary key, note text not null); insert into load_numbers select n, '=formula' from generate_series(1,100000) n; analyze load_numbers; create role load_reader login password 'synthetic-target-password'; grant connect on database portcullis to load_reader; grant usage on schema public to load_reader; grant select on load_numbers to load_reader"); err != nil {
		return err
	}
	master := make([]byte, 32)
	if _, err := rand.Read(master); err != nil {
		return err
	}
	masterText := base64.StdEncoding.EncodeToString(master)
	keyring, err := crypto.LoadKeyring(masterText, "")
	if err != nil {
		return err
	}
	store := postgres.NewIdentityStore(pool)
	org, err := store.DefaultOrganizationID(startup)
	if err != nil {
		return err
	}

	admin, err := provisionActor(startup, store, pool, org, keyring, "admin@load.invalid", "admin")
	if err != nil {
		return err
	}
	reviewer, err := provisionActor(startup, store, pool, org, keyring, "reviewer@load.invalid", "approver")
	if err != nil {
		return err
	}
	fixtures := make([]fixture, 100)
	for index := range fixtures {
		requester, err := provisionActor(startup, store, pool, org, keyring, fmt.Sprintf("requester-%d@load.invalid", index), "requester")
		if err != nil {
			return err
		}
		fixtures[index] = fixture{Requester: requester, Approver: reviewer}
	}
	logFile, err := os.OpenFile("tests/load/results/application.log", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = logFile.Close() }()
	application := settings.ApplicationBinary
	binaryDigest, err := binaryidentity.DigestFile(application)
	if err != nil {
		return err
	}
	server := exec.Command(application)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "PORTCULLIS_") {
			server.Env = append(server.Env, value)
		}
	}
	server.Env = append(server.Env, "PORTCULLIS_DATABASE_URL="+runtimeURL.String(), "PORTCULLIS_MASTER_KEY="+masterText, "PORTCULLIS_STARTUP_MIGRATE=false", "PORTCULLIS_ADDR=127.0.0.1:18082", "PORTCULLIS_SHUTDOWN_TIMEOUT=10s", "PORTCULLIS_CONNECTION_ALLOWED_CIDRS="+strings.Join(dbtest.TargetDestinationCIDRs, ","))
	server.Stdout, server.Stderr = logFile, logFile
	if err := server.Start(); err != nil {
		return err
	}
	exited := make(chan struct{})
	go func() { _ = server.Wait(); close(exited) }()
	defer func() {
		_ = server.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(15 * time.Second):
			_ = server.Process.Kill()
			<-exited
		}
	}()
	client := &http.Client{Timeout: 5 * time.Second}
	for {
		response, err := client.Get("http://127.0.0.1:18082/readyz")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == 200 {
				break
			}
		}
		select {
		case <-startup.Done():
			return errors.New("load application did not become ready")
		case <-time.After(200 * time.Millisecond):
		}
	}
	rpc := func(name, body string) (map[string]any, error) {
		request, err := http.NewRequestWithContext(startup, http.MethodPost, "http://127.0.0.1:18082/portcullis.v1."+name, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Connect-Protocol-Version", "1")
		request.Header.Set("Cookie", "__Host-portcullis_session="+admin.Session+"; __Host-portcullis_csrf="+admin.CSRF)
		request.Header.Set("X-CSRF-Token", admin.CSRF)
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode != 200 {
			return nil, fmt.Errorf("fixture %s: HTTP %d", name, response.StatusCode)
		}
		var result map[string]any
		err = json.NewDecoder(response.Body).Decode(&result)
		return result, err
	}
	host, err := targetDatabase.Host(startup)
	if err != nil {
		return err
	}
	port, err := targetDatabase.MappedPort(startup, "5432/tcp")
	if err != nil {
		return err
	}
	portNumber, err := strconv.Atoi(port.Port())
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"displayName": "Load target", "config": map[string]any{"host": host, "port": portNumber, "database": "portcullis", "user": "load_reader", "password": "synthetic-target-password", "tlsMode": "disable"}})
	if err != nil {
		return err
	}
	connection, err := rpc("Connections/Create", string(body))
	if err != nil {
		return err
	}
	descriptor, ok := connection["connection"].(map[string]any)
	if !ok {
		return errors.New("missing load connection")
	}
	id, ok := descriptor["id"].(string)
	if !ok {
		return errors.New("missing load connection id")
	}
	policyBody, err := json.Marshal(map[string]any{"connectionId": id, "expectedVersion": "1", "read": map[string]any{"allowed": true, "requiredApprovals": 1}, "write": map[string]any{}, "ddl": map[string]any{}, "queryTimeoutSeconds": 30, "maxRows": 10000, "maxResultBytes": "26214400"})
	if err != nil {
		return err
	}
	if _, err := rpc("ConnectionPolicies/Update", string(policyBody)); err != nil {
		return err
	}
	for index := range fixtures {
		fixtures[index].ConnectionID = id
	}
	fixtureJSON, err := json.Marshal(fixtures)
	if err != nil {
		return err
	}
	if err := os.WriteFile("tests/load/fixtures.local.json", fixtureJSON, 0600); err != nil {
		return err
	}
	defer func() { _ = os.Remove("tests/load/fixtures.local.json") }()
	manifest, err := json.Marshal(map[string]any{"pid": server.Process.Pid, "harnessPid": os.Getpid(), "runLabel": settings.RunLabel, "appOS": runtime.GOOS, "appArch": runtime.GOARCH, "appSHA256": binaryDigest, "postgresImage": dbtest.PostgresImage, "logicalCPUs": runtime.NumCPU(), "metadataContainer": metadataDatabase.GetContainerID(), "targetContainer": targetDatabase.GetContainerID(), "baseURL": "http://127.0.0.1:18082", "targetRows": 100000, "requesters": 100, "runtimePrivileged": false})
	if err != nil {
		return err
	}
	if err := os.WriteFile("tests/load/results/fixture.json", manifest, 0600); err != nil {
		return err
	}
	fmt.Println("LOAD_READY: least-privilege application, two owned databases, 100 synthetic requesters")
	select {
	case <-ctx.Done():
		return nil
	case <-exited:
		return errors.New("load application exited unexpectedly")
	}
}
