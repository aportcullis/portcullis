package connectapi

import (
	"connectrpc.com/connect"
	"context"
	"errors"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/platform/reqmeta"
	"net"
	"time"
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

// WrapStreamingHandler enforces the session boundary before a stream handler runs.
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
		token, csrf := sessionAndCSRFCookies(conn.RequestHeader())
		if token == "" {
			return connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
		}
		user, session, err := s.sessions.Authenticate(ctx, token)
		if err != nil {
			return streamAuthenticationError(err)
		}
		if csrf == "" || conn.RequestHeader().Get(csrfHeader) != csrf {
			return connect.NewError(connect.CodePermissionDenied, errors.New("invalid CSRF token"))
		}
		if err := s.sessions.VerifyCSRF(token, csrf); err != nil {
			if errors.Is(err, identity.ErrCSRFKeyVersionUnknown) {
				return connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
			}
			return connect.NewError(connect.CodePermissionDenied, errors.New("invalid CSRF token"))
		}
		if err := s.sessions.SlideIdle(ctx, session); err != nil {
			return streamAuthenticationError(err)
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
		ctx = context.WithValue(ctx, ctxUser, user)
		ctx = context.WithValue(ctx, ctxSessionToken, token)
		ctx = reqmeta.WithClientIP(ctx, ip)
		guard := &sessionStream{StreamingHandlerConn: conn, sessions: s.sessions, token: token, ctx: ctx, lastChecked: time.Now()}
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
			return streamAuthenticationError(err)
		}
		s.lastChecked = time.Now()
	}
	return s.StreamingHandlerConn.Send(message)
}

func streamAuthenticationError(err error) error {
	if isAuthFailure(err) {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	return connect.NewError(connect.CodeUnavailable, errors.New("temporarily unavailable"))
}
