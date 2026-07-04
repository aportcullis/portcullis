package server

// White-box test: the embedded dist FS (assets.Dist) is baked in at build time
// and can't be injected through the public constructor, so the SPA fallback
// rules are pinned here against an in-memory FS. Everything else in this
// package is tested black-box.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// Real files serve directly; everything else — client routes AND directories —
// falls back to index.html. A directory must never render http.FileServer's
// auto-generated listing (ADR-0010: unknown paths serve index.html), which
// would leak the bundle layout and break client-side routing.
func TestSPAHandlerFallsBackToIndexForDirectories(t *testing.T) {
	t.Parallel()
	const index = "<html>app</html>"
	h := spaHandler(fstest.MapFS{
		"index.html":    {Data: []byte(index)},
		"assets/app.js": {Data: []byte("js-bundle")},
	})

	cases := []struct {
		path, want string
	}{
		{"/", index},
		{"/assets/app.js", "js-bundle"},
		{"/assets/", index},    // directory → SPA fallback, never a listing
		{"/assets", index},     // extensionless directory path = client route
		{"/some/route", index}, // unknown path = client route
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		resp := rec.Result()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", tc.path, resp.StatusCode)
			continue
		}
		if got := string(body); got != tc.want {
			if strings.Contains(got, "<a href=") {
				t.Errorf("GET %s rendered a directory listing", tc.path)
			} else {
				t.Errorf("GET %s body = %q, want %q", tc.path, got, tc.want)
			}
		}
	}
}
