package server

// HTTP server size caps (ADR-0010 "HTTP server" table), co-located so the request
// and header bounds stay one policy. Both are 64 KiB: Portcullis RPCs carry only
// compact cookies, metadata headers, and tiny auth/health bodies.

// MaxRequestBytes caps an inbound RPC request body. Connect defaults to unlimited,
// so a large Bootstrap/Login body could OOM the process. Exported for the
// composition root, which applies it via connect.WithReadMaxBytes and
// http.MaxBytesHandler on each mount.
const MaxRequestBytes = 64 << 10

// maxHeaderBytes is deliberately much smaller than net/http's 1 MiB default,
// bounding header-based memory and log amplification.
const maxHeaderBytes = 64 << 10
