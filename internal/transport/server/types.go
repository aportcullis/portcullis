package server

import (
	"log/slog"
	"net/http"
	"time"
)

// Mount attaches an HTTP route — typically a Connect RPC handler — to the server.
type Mount struct {
	Pattern string
	Handler http.Handler
}

// HostPolicy decides which Host headers address this installation and which other browser origins it trusts (ADR-0052).
type HostPolicy interface {
	AllowsHost(host string) bool
	TrustedOrigins() []string
}

// Options configures the listener, shutdown drain and browser-origin boundary of a Server.
type Options struct {
	// Addr is the listen address.
	Addr string
	// Logger records lifecycle events and request logs.
	Logger *slog.Logger
	// DrainDelay is how long readiness reports "draining" before connections are closed, giving Kubernetes time to deregister the pod.
	DrainDelay time.Duration
	// Hosts admits Host headers for every route except the health probes.
	Hosts HostPolicy
	// BrowserOriginRequiredPaths are exact request paths whose unsafe-method requests must carry Origin or Sec-Fetch-Site, so a pre-session credential RPC cannot be driven by a request lacking browser provenance.
	BrowserOriginRequiredPaths []string
}
