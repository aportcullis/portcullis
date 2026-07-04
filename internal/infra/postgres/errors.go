package postgres

import "errors"

// ErrRuntimeInsecure marks a VerifyRuntimeConnection failure that is an
// OVER-privilege violation — the class an operator may knowingly accept for
// local single-role development (PORTCULLIS_ALLOW_PRIVILEGED_RUNTIME). A wrong
// or unmigrated database, a missing REQUIRED privilege (the server could not
// function), or a query failure is NOT wrapped with it and stays fatal.
var ErrRuntimeInsecure = errors.New("runtime connection is over-privileged")
