package logging

// requestIDKey is the context key under which the per-request id is stored.
const requestIDKey ctxKey = iota

// maxRequestIDLength bounds client-provided correlation ids before they are
// echoed, logged, and copied into audit rows.
const maxRequestIDLength = 128

// Supported output formats (see ValidFormat / New).
const (
	formatJSON = "json"
	formatText = "text"
)
