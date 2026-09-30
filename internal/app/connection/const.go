package connection

import "time"

// detachedWriteTimeout bounds the best-effort audit writes that detach from the request context (context.WithoutCancel) so a client disconnect cannot erase the CONNECTION_TEST trail — same posture as the auth service's failed-login events (ADR-0009).
const detachedWriteTimeout = 5 * time.Second
