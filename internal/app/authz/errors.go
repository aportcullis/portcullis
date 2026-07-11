package authz

import "errors"

// ErrPermissionDenied is returned by Authorize when the user's effective
// permissions do not include the required key. It is the ONLY authz error the
// transport maps to CodePermissionDenied — every other error (an unknown key, a
// resolver failure) is an internal or retryable condition, never a "forbidden", so
// a code defect or a database blip can never be silently read as a legitimate
// denial.
var ErrPermissionDenied = errors.New("authz: permission denied")

// ErrUnknownPermission is returned when Authorize is asked to check a key that is
// not in the loaded catalog — a typo'd or stale key at an enforcement site. It
// surfaces the code defect loudly (mapped to CodeInternal, logged) instead of
// letting a permission no user can ever hold degrade into a silent always-deny.
var ErrUnknownPermission = errors.New("authz: permission not in catalog")
