package connectapi_test

import (
	"strings"
	"testing"

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
