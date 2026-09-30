package server

// White-box tests inject an in-memory SPA filesystem because the public constructor uses build-time embedded assets.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

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
		{"/assets/", index},
		{"/assets", index},
		{"/some/route", index},
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
