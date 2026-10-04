package connectapi

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
)

// hiddenCause keeps a server fault's root cause reachable through errors.Unwrap for the error log while clients only ever see the generic message.
type hiddenCause struct {
	message string
	cause   error
}

func (h hiddenCause) Error() string { return h.message }

func (h hiddenCause) Unwrap() error { return h.cause }

// sqlStateCarrier is satisfied by driver errors that expose a SQLSTATE, such as *pgconn.PgError, without transport importing a driver.
type sqlStateCarrier interface {
	SQLState() string
}

// newServerFaultError builds a server-fault Connect error whose wire message is generic and whose cause stays available to the error log.
func newServerFaultError(code connect.Code, message string, cause error) *connect.Error {
	if cause == nil {
		return connect.NewError(code, errors.New(message))
	}
	return connect.NewError(code, hiddenCause{message: message, cause: cause})
}

// newInternalError maps an unexpected failure to the generic CodeInternal response while keeping its cause for the log.
func newInternalError(cause error) *connect.Error {
	return newServerFaultError(connect.CodeInternal, "internal error", cause)
}

// classifyErrorCause returns log fields that name a failure without its message: the innermost error type, any SQLSTATE, and cancellation or deadline causes. Messages are excluded because driver and storage errors can echo SQL, parameters, DSNs, or credentials (docs/conventions/security.md).
func classifyErrorCause(err error) []any {
	fields := []any{"error_type", fmt.Sprintf("%T", innermostError(err))}
	var carrier sqlStateCarrier
	if errors.As(err, &carrier) && carrier.SQLState() != "" {
		fields = append(fields, "sqlstate", carrier.SQLState())
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		fields = append(fields, "cause", "deadline_exceeded")
	case errors.Is(err, context.Canceled):
		fields = append(fields, "cause", "canceled")
	}
	return fields
}

// innermostError follows the wrap chain to its root, taking the first branch of a joined error.
func innermostError(err error) error {
	for {
		switch wrapped := err.(type) {
		case interface{ Unwrap() []error }:
			branches := wrapped.Unwrap()
			if len(branches) == 0 || branches[0] == nil {
				return err
			}
			err = branches[0]
		case interface{ Unwrap() error }:
			next := wrapped.Unwrap()
			if next == nil {
				return err
			}
			err = next
		default:
			return err
		}
	}
}
