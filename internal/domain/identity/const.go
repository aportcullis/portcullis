package identity

// Account lifecycle states. These are fixed domain enums the code branches on
// (see User.Active), so they live in code. Everything configurable — the
// permission catalog, roles, the OIDC issuer/provider — comes from the database
// or config, not constants.
const (
	StatusActive   UserStatus = "active"
	StatusDisabled UserStatus = "disabled"
)

// MaxEmailLength is the RFC 5321 maximum for a forward path (address). It lives
// in the domain because every consumer of an email — validation, storage, and
// rate-limit keying — shares the same bound (see EmailTooLong/NormalizeEmail).
const MaxEmailLength = 254
