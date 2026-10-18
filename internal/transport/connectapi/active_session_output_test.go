package connectapi_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/transport/connectapi"
)

type grantlessAuthorization struct{}

// Authorize refuses every protected permission while leaving authenticated Me available.
func (grantlessAuthorization) Authorize(context.Context, identity.User, identity.Permission) error {
	return authz.ErrPermissionDenied
}

type changingSessionMetadata struct {
	portcullisv1connect.UnimplementedAuthHandler
	sessions  *scriptedSessions
	afterRead error
}

// Me coordinates a session change after private metadata has been read but before it is emitted.
func (handler changingSessionMetadata) Me(context.Context, *connect.Request[portcullisv1.MeRequest]) (*connect.Response[portcullisv1.MeResponse], error) {
	handler.sessions.authenticateErr = handler.afterRead
	return connect.NewResponse(&portcullisv1.MeResponse{User: &portcullisv1.User{Id: "user-1", DisplayName: "private-session-metadata"}}), nil
}

func TestActiveSessionMetadataRequiresCurrentSessionAtOutput(t *testing.T) {
	for _, scenario := range []struct {
		name, cookie, csrf string
		afterRead          error
		want               connect.Code
	}{
		{"grantless current session", "__Host-portcullis_session=session-token; __Host-portcullis_csrf=csrf-token", "csrf-token", nil, 0},
		{"unrelated cookie", "other=1; __Host-portcullis_session=session-token; __Host-portcullis_csrf=csrf-token", "csrf-token", nil, 0},
		{"missing session", "", "", nil, connect.CodeUnauthenticated},
		{"missing CSRF", "__Host-portcullis_session=session-token; __Host-portcullis_csrf=csrf-token", "", nil, connect.CodePermissionDenied},
		{"revoked after read", "__Host-portcullis_session=session-token; __Host-portcullis_csrf=csrf-token", "csrf-token", identity.ErrSessionNotFound, connect.CodeUnauthenticated},
		{"disabled after read", "__Host-portcullis_session=session-token; __Host-portcullis_csrf=csrf-token", "csrf-token", identity.ErrUserDisabled, connect.CodeUnauthenticated},
		{"lookup failed after read", "__Host-portcullis_session=session-token; __Host-portcullis_csrf=csrf-token", "csrf-token", errors.New("metadata unavailable"), connect.CodeUnavailable},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			sessions := &scriptedSessions{}
			_, handler := portcullisv1connect.NewAuthHandler(changingSessionMetadata{sessions: sessions, afterRead: scenario.afterRead}, connect.WithInterceptors(connectapi.NewAuthInterceptor(sessions), connectapi.NewActiveBoundaryInterceptor(grantlessAuthorization{}, sessions)))
			server := httptest.NewServer(handler)
			defer server.Close()
			client := portcullisv1connect.NewAuthClient(server.Client(), server.URL)
			request := connect.NewRequest(&portcullisv1.MeRequest{})
			setSessionHeaders(request.Header(), scenario.cookie, scenario.csrf)
			response, err := client.Me(context.Background(), request)
			if codeOrSuccess(err) != scenario.want {
				t.Fatalf("Me output code=%v want %v", codeOrSuccess(err), scenario.want)
			}
			if scenario.want != 0 && response != nil {
				t.Fatal("private session metadata escaped refused output")
			}
		})
	}
}
