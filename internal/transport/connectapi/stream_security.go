package connectapi

import (
	"context"
	"errors"
	"net"
	"time"

	"connectrpc.com/connect"

	"github.com/aportcullis/portcullis/internal/platform/reqmeta"
)

// StreamSecurityInterceptor authenticates streams and rechecks revoked sessions.
type StreamSecurityInterceptor struct {
	sessions sessionAuthenticator
	trusted  []*net.IPNet
	limiter  *rateLimiter
}

// NewStreamSecurityInterceptor protects server streams with CSRF, admission, and a lifetime bound.
func NewStreamSecurityInterceptor(sessions sessionAuthenticator, trusted []*net.IPNet) *StreamSecurityInterceptor {
	return &StreamSecurityInterceptor{sessions: sessions, trusted: trusted, limiter: newRateLimiter(authRefill, authBurst, rateLimiterTTL, maxLimiterBuckets)}
}

// WrapUnary leaves unary authentication to the existing interceptor chain.
func (s *StreamSecurityInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc { return next }

// WrapStreamingClient leaves outbound calls unchanged.
func (s *StreamSecurityInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler runs the shared session pipeline before a stream handler runs, then bounds the stream's lifetime and revalidates the session while sending.
func (s *StreamSecurityInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) (returned error) {
		defer func() {
			if recover() != nil {
				returned = connect.NewError(connect.CodeInternal, errors.New("stream failed"))
			}
		}()
		ip := canonicalIP(clientIP(conn.Peer().Addr, conn.RequestHeader(), s.trusted))
		if !s.limiter.allow(ip) {
			return connect.NewError(connect.CodeResourceExhausted, errors.New("temporarily busy"))
		}
		session, err := authenticateSessionRequest(ctx, s.sessions, conn.RequestHeader())
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
		ctx = reqmeta.WithClientIP(withAuthenticatedSession(ctx, session), ip)
		guard := &sessionStream{StreamingHandlerConn: conn, sessions: s.sessions, token: session.token, ctx: ctx, lastChecked: time.Now()}
		return next(ctx, guard)
	}
}

type sessionStream struct {
	connect.StreamingHandlerConn
	sessions    sessionAuthenticator
	token       string
	ctx         context.Context
	lastChecked time.Time
}

func (s *sessionStream) Send(message any) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if time.Since(s.lastChecked) >= time.Minute {
		_, _, err := s.sessions.Authenticate(s.ctx, s.token)
		if err != nil {
			return sessionAuthenticationError(err)
		}
		s.lastChecked = time.Now()
	}
	return s.StreamingHandlerConn.Send(message)
}
