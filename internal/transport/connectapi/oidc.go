package connectapi

// The Google login routes are plain HTTP handlers, not Connect RPCs: the flow
// is a browser redirect dance (302 out to Google, 302 back), which doesn't fit
// a unary RPC. They live in this package to share the cookie helpers, client-IP
// resolution, and rate limiter with the Connect handlers instead of exporting
// those internals (ADR-0007).

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/platform/reqmeta"
)

// PendingCodec seals and opens the OIDC pending cookie payload — the
// transport's consumer-defined port onto the AEAD cookie codec (infra/crypto
// provides the keyring-backed adapter; Open fails closed on any malformation).
type PendingCodec interface {
	Seal(plaintext []byte) (string, error)
	Open(value string) ([]byte, error)
}

// OIDCHandler serves the two Google login routes. Both are unauthenticated
// public endpoints, so each is wrapped with client-IP resolution (for the
// audit trail's source_ip) and the tight login-tier per-IP rate limit — the
// callback in particular triggers an outbound code exchange per hit.
type OIDCHandler struct {
	svc     *auth.Service
	codec   PendingCodec
	trusted []*net.IPNet
	limiter *rateLimiter
	logger  *slog.Logger
}

// oidcPendingPayload is the wire format inside the sealed pending cookie. The
// expiry rides INSIDE the ciphertext so the client-controlled cookie Max-Age is
// never the only check (ADR-0007).
type oidcPendingPayload struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	Exp      int64  `json:"exp"` // unix seconds
}

// NewOIDCHandler builds the Google login routes over the auth service.
func NewOIDCHandler(svc *auth.Service, codec PendingCodec, trustedProxies []*net.IPNet, logger *slog.Logger) *OIDCHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &OIDCHandler{
		svc:     svc,
		codec:   codec,
		trusted: trustedProxies,
		limiter: newRateLimiter(loginRefill, loginBurst, rateLimiterTTL, maxLimiterBuckets),
		logger:  logger,
	}
}

// Start handles OIDCStartPattern: mint the per-flow secrets, seal them into
// the pending cookie, and redirect to Google.
func (h *OIDCHandler) Start() http.Handler { return h.wrap(h.start) }

// Callback handles OIDCCallbackPattern: complete the login and redirect to the
// SPA — "/" with the session cookies on success, the login page on any failure.
func (h *OIDCHandler) Callback() http.Handler { return h.wrap(h.callback) }

// wrap mirrors the Connect chain's ordering for a plain route: resolve the
// client IP into the context first (rate-limit key + audit source_ip), then
// shed over-limit requests with a plain 429 — not a redirect, so throttling is
// never masked as a login failure and can't loop through /login.
func (h *OIDCHandler) wrap(next func(http.ResponseWriter, *http.Request)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := canonicalIP(clientIP(r.RemoteAddr, r.Header, h.trusted))
		if ip != "" && !h.limiter.allow("ip:"+ip) {
			http.Error(w, "too many attempts, retry later", http.StatusTooManyRequests)
			return
		}
		next(w, r.WithContext(reqmeta.WithClientIP(r.Context(), ip)))
	})
}

func (h *OIDCHandler) start(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	authURL, pending, err := h.svc.StartGoogleLogin(ctx)
	if err != nil {
		h.failLogin(w, r, "start", err)
		return
	}
	payload, err := json.Marshal(oidcPendingPayload{
		State:    pending.State,
		Nonce:    pending.Nonce,
		Verifier: pending.Verifier,
		Exp:      pending.ExpiresAt.Unix(),
	})
	if err != nil {
		h.failLogin(w, r, "start", err)
		return
	}
	sealed, err := h.codec.Seal(payload)
	if err != nil {
		h.failLogin(w, r, "start", err)
		return
	}
	noStore(w.Header())
	http.SetCookie(w, &http.Cookie{
		Name:     oidcPendingCookie,
		Value:    sealed,
		Path:     "/",
		MaxAge:   int(time.Until(pending.ExpiresAt).Seconds()),
		Secure:   true,
		HttpOnly: true,
		// Lax sends the cookie on the top-level GET back from Google.
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (h *OIDCHandler) callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	noStore(w.Header())
	// The pending cookie is single-use: clear it whatever happens next, so a
	// replayed callback can't reuse the flow.
	clearCookie(w.Header(), oidcPendingCookie, true)

	// A missing or unopenable cookie decodes to the zero pending, which the
	// service rejects AND audits — one failure path for every malformed callback,
	// including Google's error= responses (no code fails the same way).
	var pending auth.OIDCPending
	if c, err := r.Cookie(oidcPendingCookie); err == nil {
		if plaintext, err := h.codec.Open(c.Value); err == nil {
			var p oidcPendingPayload
			if err := json.Unmarshal(plaintext, &p); err == nil {
				pending = auth.OIDCPending{State: p.State, Nonce: p.Nonce, Verifier: p.Verifier, ExpiresAt: time.Unix(p.Exp, 0)}
			}
		}
	}

	q := r.URL.Query()
	sess, err := h.svc.LoginWithGoogle(ctx, q.Get("state"), q.Get("code"), pending)
	if err != nil {
		h.failLogin(w, r, "callback", err)
		return
	}
	setCookie(w.Header(), sessionCookie, sess.Token, true, sess.Session.AbsoluteExpiresAt)
	setCookie(w.Header(), csrfCookie, sess.CSRF, false, sess.Session.AbsoluteExpiresAt)
	http.Redirect(w, r, oidcSuccessRedirect, http.StatusFound)
}

// failLogin logs the failure (error type only — never the code, tokens, state,
// or claim values) and sends the browser to the login page with a generic
// error marker.
func (h *OIDCHandler) failLogin(w http.ResponseWriter, r *http.Request, step string, err error) {
	h.logger.WarnContext(r.Context(), "google login failed", "step", step, "error_type", fmt.Sprintf("%T", err))
	http.Redirect(w, r, oidcFailureRedirect, http.StatusFound)
}
