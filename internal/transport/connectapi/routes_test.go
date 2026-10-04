package connectapi_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	"github.com/aportcullis/portcullis/internal/platform/config"
	"github.com/aportcullis/portcullis/internal/transport/connectapi"
)

func TestGoogleCallbackPathMatchesConfigConstant(t *testing.T) {
	t.Parallel()
	path := strings.TrimPrefix(connectapi.OIDCCallbackPattern, "GET ")
	if path != config.GoogleCallbackPath {
		t.Fatalf("OIDCCallbackPattern path = %q, config.GoogleCallbackPath = %q — update both together", path, config.GoogleCallbackPath)
	}
}

func TestBrowserOriginRequiredProceduresCoverPreSessionCredentialsOnly(t *testing.T) {
	t.Parallel()
	want := []string{portcullisv1connect.AuthBootstrapProcedure, portcullisv1connect.AuthLoginProcedure}
	if got := connectapi.BrowserOriginRequiredProcedures(); !slices.Equal(got, want) {
		t.Fatalf("BrowserOriginRequiredProcedures = %q, want %q (GetConfig is a read and authenticated RPCs carry CSRF tokens)", got, want)
	}
}
