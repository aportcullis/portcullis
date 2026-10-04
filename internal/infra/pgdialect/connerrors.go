package pgdialect

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aportcullis/portcullis/internal/domain/connection"
)

// PostgreSQL SQLSTATE codes surfaced during connection/authentication.
const (
	sqlstateInvalidPassword       = "28P01" // wrong password
	sqlstateInvalidAuthorization  = "28000" // rejected by pg_hba / role cannot log in
	sqlstateInvalidCatalogName    = "3D000" // database does not exist
	sqlstateInsufficientPrivilege = "42501" // e.g. CONNECT revoked
)

// classify maps a dial/auth error onto the caller-safe test buckets (ADR-0014). The raw error — which can embed driver text and, in principle, credential material — never crosses this function (PRD §8.1).
func classify(err error) *connection.TestError {
	if errors.Is(err, errDestinationRefused) {
		return &connection.TestError{Bucket: connection.TestBucketDestinationRefused}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &connection.TestError{Bucket: connection.TestBucketTimeout}
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case sqlstateInvalidPassword, sqlstateInvalidAuthorization, sqlstateInsufficientPrivilege:
			return &connection.TestError{Bucket: connection.TestBucketAuthFailed}
		case sqlstateInvalidCatalogName:
			return &connection.TestError{Bucket: connection.TestBucketUnknownDatabase}
		}
		return &connection.TestError{Bucket: connection.TestBucketFailed}
	}

	if isTLSFailure(err) {
		return &connection.TestError{Bucket: connection.TestBucketTLSFailed}
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return &connection.TestError{Bucket: connection.TestBucketTimeout}
		}
		return &connection.TestError{Bucket: connection.TestBucketUnreachable}
	}

	return &connection.TestError{Bucket: connection.TestBucketFailed}
}

// isTLSFailure recognizes certificate-validation and TLS-negotiation failures.
func isTLSFailure(err error) bool {
	var (
		certVerify *tls.CertificateVerificationError
		recordHdr  tls.RecordHeaderError
		hostname   x509.HostnameError
		unknownCA  x509.UnknownAuthorityError
		invalid    x509.CertificateInvalidError
	)
	if errors.As(err, &certVerify) || errors.As(err, &recordHdr) ||
		errors.As(err, &hostname) || errors.As(err, &unknownCA) || errors.As(err, &invalid) {
		return true
	}
	// pgconn reports a server that answers 'N' to the SSLRequest with a plain, unexported error ("server refused TLS connection" — pgconn v5 source); message matching is fragile but contained here and pinned by an integration test against a non-TLS server.
	return strings.Contains(err.Error(), "refused TLS")
}
