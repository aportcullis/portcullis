package postgres

import "errors"

// ErrRuntimeInsecure identifies excess privileges that development mode may allow; wrong databases, missing grants, and query failures remain fatal.
var ErrRuntimeInsecure = errors.New("runtime connection is over-privileged")
