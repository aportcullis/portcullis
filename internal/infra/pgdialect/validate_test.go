package pgdialect_test

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/aportcullis/portcullis/internal/infra/pgdialect"
)

func pgCoords(t *testing.T) (target connection.Target, cred connection.Credential) {
	t.Helper()
	cc := dbtest.TargetPostgres(t).Config().ConnConfig
	target, err := connection.NewTarget(cc.Host, int(cc.Port), cc.Database)
	if err != nil {
		t.Fatal(err)
	}
	cred, err = connection.NewCredential(cc.User, cc.Password)
	if err != nil {
		t.Fatal(err)
	}
	return target, cred
}

func assertBucket(t *testing.T, err error, want connection.TestBucket, password string) {
	t.Helper()
	var te *connection.TestError
	if !errors.As(err, &te) {
		t.Fatalf("err = %v (%T), want *connection.TestError", err, err)
	}
	if te.Bucket != want {
		t.Fatalf("bucket = %q, want %q", te.Bucket, want)
	}
	if got := te.Error(); got != "connection test failed: "+string(want) {
		t.Errorf("error text = %q — must carry nothing but the bucket", got)
	}
	if password != "" && strings.Contains(err.Error(), password) {
		t.Error("error text contains the password")
	}
}

func TestValidateConnectionSucceedsAgainstRealTarget(t *testing.T) {
	t.Parallel()
	target, cred := pgCoords(t)
	validator := pgdialect.New(pgdialect.Options{ValidateTimeout: 10 * time.Second})

	if err := validator.ValidateConnection(context.Background(), target, connection.TLSModeDisable, cred); err != nil {
		t.Fatalf("Test: %v", err)
	}
}

func TestValidateConnectionIgnoresProcessEnvironment(t *testing.T) {
	cases := []struct{ env, val string }{
		{"PGSSLROOTCERT", "/nonexistent/portcullis-test-ca.pem"},
		{"PGSSLCERT", "/nonexistent/portcullis-test-client.pem"},
		{"PGSSLKEY", "/nonexistent/portcullis-test-client.key"},
		{"PGSSLMODE", "verify-full"},
		{"PGSSLNEGOTIATION", "direct"},
		{"PGSSLSNI", "0"},
		{"PGOPTIONS", "-c statement_timeout=1"},
		{"PGTARGETSESSIONATTRS", "read-only"},
		{"PGCHANNELBINDING", "require"},
		{"PGREQUIREAUTH", "scram-sha-256"},
		{"PGMINPROTOCOLVERSION", "3.2"},
		{"PGMAXPROTOCOLVERSION", "3.2"},
		// ParseConfig itself must not fail from the environment either: an invalid PGCONNECT_TIMEOUT or a dangling PGSERVICE/PGSERVICEFILE would otherwise misclassify every test as a config parse failure.
		{"PGCONNECT_TIMEOUT", "not-a-duration"},
		{"PGSERVICE", "portcullis-nonexistent-service"},
		{"PGSERVICEFILE", "/nonexistent/portcullis-service.conf"},
		{"PGPASSFILE", "/nonexistent/portcullis-pgpass"},
	}
	for _, tc := range cases {
		t.Run(tc.env, func(t *testing.T) {
			target, cred := pgCoords(t)
			t.Setenv(tc.env, tc.val)
			validator := pgdialect.New(pgdialect.Options{ValidateTimeout: 10 * time.Second})
			if err := validator.ValidateConnection(context.Background(), target, connection.TLSModeDisable, cred); err != nil {
				t.Fatalf("Test with %s=%s in the environment: %v", tc.env, tc.val, err)
			}
		})
	}
}

func TestValidateConnectionClassifiesWrongPassword(t *testing.T) {
	t.Parallel()
	target, cred := pgCoords(t)
	bad, err := connection.NewCredential(cred.User, "definitely-wrong-password")
	if err != nil {
		t.Fatal(err)
	}
	validator := pgdialect.New(pgdialect.Options{ValidateTimeout: 10 * time.Second})
	got := validator.ValidateConnection(context.Background(), target, connection.TLSModeDisable, bad)
	assertBucket(t, got, connection.TestBucketAuthFailed, "definitely-wrong-password")
}

func TestValidateConnectionClassifiesUnknownDatabase(t *testing.T) {
	t.Parallel()
	target, cred := pgCoords(t)
	missing, err := connection.NewTarget(target.Host, int(target.Port), "portcullis_no_such_db")
	if err != nil {
		t.Fatal(err)
	}
	validator := pgdialect.New(pgdialect.Options{ValidateTimeout: 10 * time.Second})
	got := validator.ValidateConnection(context.Background(), missing, connection.TLSModeDisable, cred)
	assertBucket(t, got, connection.TestBucketUnknownDatabase, cred.Password)
}

func TestValidateConnectionClassifiesTLSFailure(t *testing.T) {
	t.Parallel()
	target, cred := pgCoords(t)
	validator := pgdialect.New(pgdialect.Options{ValidateTimeout: 10 * time.Second})
	got := validator.ValidateConnection(context.Background(), target, connection.TLSModeVerifyFull, cred)
	assertBucket(t, got, connection.TestBucketTLSFailed, cred.Password)
}

func TestValidateConnectionClassifiesUnreachable(t *testing.T) {
	t.Parallel()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, portStr, _ := net.SplitHostPort(l.Addr().String())
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)

	target, err := connection.NewTarget("127.0.0.1", port, "db")
	if err != nil {
		t.Fatal(err)
	}
	cred, err := connection.NewCredential("u", "pw-unreachable")
	if err != nil {
		t.Fatal(err)
	}
	validator := pgdialect.New(pgdialect.Options{ValidateTimeout: 5 * time.Second})
	got := validator.ValidateConnection(context.Background(), target, connection.TLSModeDisable, cred)
	assertBucket(t, got, connection.TestBucketUnreachable, "pw-unreachable")
}

func TestValidateConnectionHonorsTimeout(t *testing.T) {
	t.Parallel()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close() //nolint:errcheck // test listener teardown
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}

			defer conn.Close() //nolint:errcheck // held-open test connection
		}
	}()
	_, portStr, _ := net.SplitHostPort(l.Addr().String())
	port, _ := strconv.Atoi(portStr)

	target, err := connection.NewTarget("127.0.0.1", port, "db")
	if err != nil {
		t.Fatal(err)
	}
	cred, err := connection.NewCredential("u", "pw-timeout")
	if err != nil {
		t.Fatal(err)
	}
	validator := pgdialect.New(pgdialect.Options{ValidateTimeout: time.Second})
	start := time.Now()
	got := validator.ValidateConnection(context.Background(), target, connection.TLSModeDisable, cred)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("test took %s, want ~1s timeout", elapsed)
	}
	assertBucket(t, got, connection.TestBucketTimeout, "pw-timeout")
}
