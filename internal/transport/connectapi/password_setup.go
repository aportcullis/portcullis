package connectapi

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// CompletePasswordSetup consumes the bearer credential without creating or rotating a session.
func (a *AuthService) CompletePasswordSetup(ctx context.Context, request *connect.Request[portcullisv1.CompletePasswordSetupRequest]) (*connect.Response[portcullisv1.CompletePasswordSetupResponse], error) {
	if err := a.svc.CompletePasswordSetup(ctx, request.Msg.GetToken(), request.Msg.GetPassword()); err != nil {
		if errors.Is(err, identity.ErrPasswordSetupInvalid) {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("invalid or expired password setup link"))
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, managementError(err, false)
		}
		return nil, authError(err)
	}
	response := connect.NewResponse(&portcullisv1.CompletePasswordSetupResponse{})
	noStore(response.Header())
	return response, nil
}
