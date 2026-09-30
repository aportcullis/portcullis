// Package reqmeta carries per-request metadata (currently the resolved client IP) across layers via the context, so the application layer can read it without importing the transport. The transport resolves and injects the values; app and infra only read them. It mirrors logging.RequestID.
package reqmeta

import "context"

// WithClientIP returns a context carrying the resolved client IP.
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, clientIPKey, ip)
}

// ClientIP returns the client IP carried in ctx, or "" if absent.
func ClientIP(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey).(string)
	return ip
}
