package connectapi_test

import (
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/platform/config"
	"github.com/aportcullis/portcullis/internal/transport/connectapi"
)

// The Google callback path exists as two literals: the mounted route pattern
// here and config.GoogleCallbackPath, which boot-time validation enforces on
// google_redirect_url (config is a leaf package and cannot import transport).
// This pins them together so neither can drift alone.
func TestGoogleCallbackPathMatchesConfigConstant(t *testing.T) {
	t.Parallel()
	path := strings.TrimPrefix(connectapi.OIDCCallbackPattern, "GET ")
	if path != config.GoogleCallbackPath {
		t.Fatalf("OIDCCallbackPattern path = %q, config.GoogleCallbackPath = %q — update both together", path, config.GoogleCallbackPath)
	}
}
