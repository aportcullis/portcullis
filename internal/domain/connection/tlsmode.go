package connection

// TLSMode is the transport-security posture for connecting to a target database. The accepted set is a fixed domain enum (ADR-0014): libpq's prefer/allow are deliberately absent because they silently downgrade to plaintext, which contradicts the certificate-verifying default (PRD §8.1).
type TLSMode string

// Accepted TLS modes — mirrors the connections.tls_mode check constraint.
const (
	// TLSModeVerifyFull validates the certificate chain and the hostname.
	TLSModeVerifyFull TLSMode = "verify-full"
	// TLSModeVerifyCA validates the certificate chain but not the hostname.
	TLSModeVerifyCA TLSMode = "verify-ca"
	// TLSModeRequire encrypts but performs no certificate validation — relaxed.
	TLSModeRequire TLSMode = "require"
	// TLSModeDisable uses no encryption at all — relaxed.
	TLSModeDisable TLSMode = "disable"
)

// DefaultTLSMode is the mode applied when the caller does not choose one: certificate-verifying, per PRD §8.1.
func DefaultTLSMode() TLSMode { return TLSModeVerifyFull }

// ParseTLSMode validates a wire-level TLS mode string. Empty means "no explicit choice" and resolves to the certificate-verifying default, so the default lives in exactly one place. Everything outside the accepted set — including prefer/allow — fails with ErrInvalidTLSMode.
func ParseTLSMode(s string) (TLSMode, error) {
	switch TLSMode(s) {
	case TLSModeVerifyFull, TLSModeVerifyCA, TLSModeRequire, TLSModeDisable:
		return TLSMode(s), nil
	}
	if s == "" {
		return DefaultTLSMode(), nil
	}
	return "", ErrInvalidTLSMode
}

// Relaxed reports whether the mode skips certificate-chain validation entirely. Choosing a relaxed mode requires an explicit admin decision and emits a CONNECTION_TLS_RELAXED audit event (PRD §8.1, ADR-0014). verify-ca is not relaxed — it validates the chain — but callers should surface its no-hostname-check caveat.
func (m TLSMode) Relaxed() bool {
	return m == TLSModeRequire || m == TLSModeDisable
}
