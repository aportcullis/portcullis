package connectapi

// White-box: the error-mapping helpers (requestError, connectionError, executionError, policyError, authError) are unexported, and these scenarios pin that each one keeps a classified cause for the server log while the wire stays generic.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgconn"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// secretFragments must never reach the wire or the log, whichever error carries them.
var secretFragments = []string{"hunter2", "SELECT secret_column", "postgres://admin"}

// scriptedFailureExecutions returns the scripted error from a unary and a streaming procedure, or panics when told to.
type scriptedFailureExecutions struct {
	portcullisv1connect.UnimplementedQueryExecutionsHandler
	failure error
	panics  bool
}

func (s scriptedFailureExecutions) Get(context.Context, *connect.Request[portcullisv1.GetQueryExecutionRequest]) (*connect.Response[portcullisv1.QueryExecution], error) {
	if s.failure != nil {
		return nil, s.failure
	}
	return connect.NewResponse(&portcullisv1.QueryExecution{}), nil
}

func (s scriptedFailureExecutions) ExportCSV(_ context.Context, _ *connect.Request[portcullisv1.ExportQueryCSVRequest], stream *connect.ServerStream[portcullisv1.QueryCSVChunk]) error {
	if s.panics {
		panic("password=hunter2 in a panic value")
	}
	if s.failure != nil {
		return s.failure
	}
	return stream.Send(&portcullisv1.QueryCSVChunk{Data: []byte("a\n")})
}

// acceptingSessions authenticates every presented session so failures come only from the handler.
type acceptingSessions struct{}

func (acceptingSessions) Authenticate(context.Context, string) (identity.User, identity.Session, error) {
	return identity.User{ID: "user-1", Status: identity.StatusActive}, identity.Session{ID: "session-1", UserID: "user-1"}, nil
}

func (acceptingSessions) VerifyCSRF(string, string) error { return nil }

func (acceptingSessions) SlideIdle(context.Context, identity.Session) error { return nil }

// withAcceptedSession attaches the session cookie and matching CSRF header every scripted call presents.
func withAcceptedSession[T any](req *connect.Request[T]) *connect.Request[T] {
	req.Header().Set("Cookie", sessionCookie+"=session-token; "+csrfCookie+"=csrf-token")
	req.Header().Set(csrfHeader, "csrf-token")
	return req
}

// serveScriptedFailures wires the handler like cmd/portcullis: recovery outermost, then error logging, session authentication, and the stream security interceptor.
func serveScriptedFailures(t *testing.T, handler scriptedFailureExecutions) (portcullisv1connect.QueryExecutionsClient, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	path, served := portcullisv1connect.NewQueryExecutionsHandler(handler, NewRecoverOption(logger),
		connect.WithInterceptors(NewErrorLogInterceptor(logger), NewAuthInterceptor(acceptingSessions{})),
		connect.WithInterceptors(NewStreamSecurityInterceptor(acceptingSessions{}, nil)))
	mux := http.NewServeMux()
	mux.Handle(path, served)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return portcullisv1connect.NewQueryExecutionsClient(server.Client(), server.URL), &logs
}

// callScriptedUnary returns the unary procedure's error.
func callScriptedUnary(client portcullisv1connect.QueryExecutionsClient) error {
	_, err := client.Get(context.Background(), withAcceptedSession(connect.NewRequest(&portcullisv1.GetQueryExecutionRequest{})))
	return err
}

// callScriptedStream drains the streaming procedure and returns its terminal error.
func callScriptedStream(client portcullisv1connect.QueryExecutionsClient) error {
	stream, err := client.ExportCSV(context.Background(), withAcceptedSession(connect.NewRequest(&portcullisv1.ExportQueryCSVRequest{})))
	if err != nil {
		return err
	}
	for stream.Receive() {
	}
	return stream.Err()
}

// assertNoSecrets fails when a secret fragment reached the client error or the log.
func assertNoSecrets(t *testing.T, wire error, logs string) {
	t.Helper()
	for _, fragment := range secretFragments {
		if wire != nil && strings.Contains(wire.Error(), fragment) {
			t.Errorf("wire error leaked %q: %v", fragment, wire)
		}
		if strings.Contains(logs, fragment) {
			t.Errorf("log leaked %q: %s", fragment, logs)
		}
	}
}

func TestInternalErrorsLogClassifiedCauseWithoutSecrets(t *testing.T) {
	serialization := &pgconn.PgError{Code: "40001", Message: "could not serialize: SELECT secret_column", Detail: "password=hunter2"}
	cases := []struct {
		name       string
		mapped     error
		wantCode   connect.Code
		wantFields []string
	}{
		{"request store serialization failure", requestError(fmt.Errorf("approve: %w", serialization)), connect.CodeInternal, []string{`"sqlstate":"40001"`, `"error_type":"*pgconn.PgError"`}},
		{"connection store deadline", connectionError(fmt.Errorf("load connection: %w", context.DeadlineExceeded)), connect.CodeInternal, []string{`"cause":"deadline_exceeded"`}},
		{"policy store constraint failure", policyError(fmt.Errorf("insert policy: %w", &pgconn.PgError{Code: "23514", Message: "postgres://admin@db"})), connect.CodeInternal, []string{`"sqlstate":"23514"`}},
		{"auth store opaque failure", authError(errors.Join(errors.New("dial postgres://admin@db failed"), errors.New("password=hunter2"))), connect.CodeInternal, []string{`"error_type":"*errors.errorString"`}},
		{"target unavailable keeps its driver cause", executionError(fmt.Errorf("%w: %w", access.ErrTargetUnavailable, &pgconn.PgError{Code: "57P01", Message: "SELECT secret_column"})), connect.CodeUnavailable, []string{`"sqlstate":"57P01"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range []struct {
				name string
				call func(portcullisv1connect.QueryExecutionsClient) error
			}{{"unary", callScriptedUnary}, {"stream", callScriptedStream}} {
				client, logs := serveScriptedFailures(t, scriptedFailureExecutions{failure: tc.mapped})
				err := path.call(client)
				if connect.CodeOf(err) != tc.wantCode {
					t.Fatalf("%s code = %v, want %v", path.name, connect.CodeOf(err), tc.wantCode)
				}
				logged := logs.String()
				if !strings.Contains(logged, `"msg":"rpc failed"`) || !strings.Contains(logged, "QueryExecutions/") {
					t.Fatalf("%s server fault not logged with its procedure: %s", path.name, logged)
				}
				for _, field := range tc.wantFields {
					if !strings.Contains(logged, field) {
						t.Errorf("%s log lacks %s: %s", path.name, field, logged)
					}
				}
				assertNoSecrets(t, err, logged)
			}
		})
	}
}

func TestStreamPanicsAndClientFaultsAreLoggedLikeUnary(t *testing.T) {
	client, logs := serveScriptedFailures(t, scriptedFailureExecutions{panics: true})
	err := callScriptedStream(client)
	if connect.CodeOf(err) != connect.CodeInternal {
		t.Fatalf("stream panic code = %v, want Internal", connect.CodeOf(err))
	}
	if !strings.Contains(logs.String(), "recovered from panic") || !strings.Contains(logs.String(), "ExportCSV") {
		t.Errorf("stream panic was not logged with its procedure: %s", logs.String())
	}
	assertNoSecrets(t, err, logs.String())

	for _, clientFault := range []error{
		connect.NewError(connect.CodePermissionDenied, errors.New("permission denied")),
		connect.NewError(connect.CodeNotFound, errors.New("request not found")),
		connect.NewError(connect.CodeResourceExhausted, errors.New("temporarily busy")),
	} {
		client, logs := serveScriptedFailures(t, scriptedFailureExecutions{failure: clientFault})
		if err := callScriptedStream(client); connect.CodeOf(err) != connect.CodeOf(clientFault) {
			t.Fatalf("stream client fault code = %v, want %v", connect.CodeOf(err), connect.CodeOf(clientFault))
		}
		if logs.Len() != 0 {
			t.Errorf("stream client fault %v was logged: %s", connect.CodeOf(clientFault), logs.String())
		}
	}

	client, logs = serveScriptedFailures(t, scriptedFailureExecutions{})
	if err := callScriptedStream(client); err != nil {
		t.Fatalf("successful stream: %v", err)
	}
	if logs.Len() != 0 {
		t.Errorf("successful stream produced a log line: %s", logs.String())
	}
}
