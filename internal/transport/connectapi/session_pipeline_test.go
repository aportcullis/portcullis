package connectapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/transport/connectapi"
)

// scriptedSessions is a session authenticator whose verdicts each test case scripts.
type scriptedSessions struct {
	authenticateErr error
	csrfErr         error
	slideErr        error
	slides          atomic.Int32
}

func (s *scriptedSessions) Authenticate(context.Context, string) (identity.User, identity.Session, error) {
	if s.authenticateErr != nil {
		return identity.User{}, identity.Session{}, s.authenticateErr
	}
	now := time.Now()
	return identity.User{ID: "user-1", Email: "user@example.com", Status: identity.StatusActive},
		identity.Session{ID: "session-1", UserID: "user-1", IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(24 * time.Hour)}, nil
}

func (s *scriptedSessions) VerifyCSRF(_, csrfToken string) error {
	if csrfToken != "csrf-token" {
		return identity.ErrCSRFTokenInvalid
	}
	return s.csrfErr
}

func (s *scriptedSessions) SlideIdle(context.Context, identity.Session) error {
	s.slides.Add(1)
	return s.slideErr
}

// reachableExecutions answers one unary and one streaming procedure so both authentication paths can be observed.
type reachableExecutions struct {
	portcullisv1connect.UnimplementedQueryExecutionsHandler
}

func (reachableExecutions) Get(context.Context, *connect.Request[portcullisv1.GetQueryExecutionRequest]) (*connect.Response[portcullisv1.QueryExecution], error) {
	return connect.NewResponse(&portcullisv1.QueryExecution{}), nil
}

func (reachableExecutions) ExportCSV(_ context.Context, _ *connect.Request[portcullisv1.ExportQueryCSVRequest], stream *connect.ServerStream[portcullisv1.QueryCSVChunk]) error {
	return stream.Send(&portcullisv1.QueryCSVChunk{Data: []byte("a\n")})
}

// newSessionPipelineClient serves the executions handler behind the production unary and stream session interceptors.
func newSessionPipelineClient(t *testing.T, sessions *scriptedSessions) portcullisv1connect.QueryExecutionsClient {
	t.Helper()
	path, handler := portcullisv1connect.NewQueryExecutionsHandler(reachableExecutions{},
		connect.WithInterceptors(connectapi.NewAuthInterceptor(sessions), connectapi.NewStreamSecurityInterceptor(sessions, nil)))
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return portcullisv1connect.NewQueryExecutionsClient(server.Client(), server.URL)
}

// setSessionHeaders writes the cookie and CSRF header a case presents.
func setSessionHeaders(header http.Header, cookie, csrfHeader string) {
	if cookie != "" {
		header.Set("Cookie", cookie)
	}
	if csrfHeader != "" {
		header.Set("X-CSRF-Token", csrfHeader)
	}
}

// callUnaryAndStream returns the codes the unary and the streaming procedure produce for the same presented credentials.
func callUnaryAndStream(t *testing.T, client portcullisv1connect.QueryExecutionsClient, cookie, csrfHeader string) (connect.Code, connect.Code) {
	t.Helper()
	ctx := context.Background()
	unary := connect.NewRequest(&portcullisv1.GetQueryExecutionRequest{RequestId: "request-1"})
	setSessionHeaders(unary.Header(), cookie, csrfHeader)
	_, unaryErr := client.Get(ctx, unary)
	streamRequest := connect.NewRequest(&portcullisv1.ExportQueryCSVRequest{RequestId: "request-1"})
	setSessionHeaders(streamRequest.Header(), cookie, csrfHeader)
	stream, streamErr := client.ExportCSV(ctx, streamRequest)
	if streamErr == nil {
		for stream.Receive() {
		}
		streamErr = stream.Err()
	}
	return codeOrSuccess(unaryErr), codeOrSuccess(streamErr)
}

// codeOrSuccess maps a nil error to the zero code, since connect.CodeOf(nil) reports Unknown.
func codeOrSuccess(err error) connect.Code {
	if err == nil {
		return 0
	}
	return connect.CodeOf(err)
}

func TestUnaryAndStreamSessionPipelinesAgree(t *testing.T) {
	const validCookie = "__Host-portcullis_session=session-token; __Host-portcullis_csrf=csrf-token"
	const success connect.Code = 0
	infraFailure := errors.New("metadata database unreachable")
	cases := []struct {
		name       string
		sessions   *scriptedSessions
		cookie     string
		csrfHeader string
		want       connect.Code
		wantSlides int32
	}{
		{"valid session and CSRF", &scriptedSessions{}, validCookie, "csrf-token", success, 2},
		{"session cookie beside a foreign sibling cookie", &scriptedSessions{}, "other=1; " + validCookie, "csrf-token", success, 2},
		{"missing session cookie", &scriptedSessions{}, "__Host-portcullis_csrf=csrf-token", "csrf-token", connect.CodeUnauthenticated, 0},
		{"revoked session", &scriptedSessions{authenticateErr: identity.ErrSessionNotFound}, validCookie, "csrf-token", connect.CodeUnauthenticated, 0},
		{"disabled user", &scriptedSessions{authenticateErr: identity.ErrUserDisabled}, validCookie, "csrf-token", connect.CodeUnauthenticated, 0},
		{"session lookup infrastructure failure", &scriptedSessions{authenticateErr: infraFailure}, validCookie, "csrf-token", connect.CodeUnavailable, 0},
		{"missing CSRF header", &scriptedSessions{}, validCookie, "", connect.CodePermissionDenied, 0},
		{"CSRF header differs from cookie", &scriptedSessions{}, validCookie, "forged-token", connect.CodePermissionDenied, 0},
		{"missing CSRF cookie", &scriptedSessions{}, "__Host-portcullis_session=session-token", "csrf-token", connect.CodePermissionDenied, 0},
		{"forged CSRF HMAC", &scriptedSessions{csrfErr: identity.ErrCSRFTokenInvalid}, validCookie, "csrf-token", connect.CodePermissionDenied, 0},
		{"unloaded CSRF key version", &scriptedSessions{csrfErr: identity.ErrCSRFKeyVersionUnknown}, validCookie, "csrf-token", connect.CodeUnauthenticated, 0},
		{"session died before the idle slide", &scriptedSessions{slideErr: identity.ErrSessionNotFound}, validCookie, "csrf-token", connect.CodeUnauthenticated, 2},
		{"idle slide infrastructure failure", &scriptedSessions{slideErr: infraFailure}, validCookie, "csrf-token", connect.CodeUnavailable, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newSessionPipelineClient(t, tc.sessions)
			unaryCode, streamCode := callUnaryAndStream(t, client, tc.cookie, tc.csrfHeader)
			if unaryCode != tc.want || streamCode != tc.want {
				t.Errorf("codes unary=%v stream=%v, want %v for both", unaryCode, streamCode, tc.want)
			}
			// The idle window may slide only after CSRF passes, on both paths.
			if slides := tc.sessions.slides.Load(); slides != tc.wantSlides {
				t.Errorf("idle slides = %d, want %d", slides, tc.wantSlides)
			}
		})
	}
}
