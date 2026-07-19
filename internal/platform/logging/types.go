package logging

import "net/http"

// ctxKey types the request-id context key so it can't collide with keys from
// other packages.
type ctxKey int

// recorder wraps an http.ResponseWriter to capture the status code and byte count
// for the per-request log line. Its methods live in logging.go with the middleware.
type recorder struct {
	http.ResponseWriter
	status int
	bytes  int64
	// wroteHeader pins status to the FIRST WriteHeader call: net/http sends only
	// the first status to the client and ignores the rest, so recording later
	// calls would log a status the client never received.
	wroteHeader bool
}
