package authz

import "errors"

// ErrPermissionDenied means a resolved user lacks permission. Unknown keys and resolver failures must surface as server errors, never denial.
var ErrPermissionDenied = errors.New("authz: permission denied")

// ErrUnknownPermission is returned when Authorize is asked to check a key that is not in the loaded catalog — a typo'd or stale key at an enforcement site. It surfaces the code defect loudly (mapped to CodeInternal, logged) instead of letting a permission no user can ever hold degrade into a silent always-deny.
var ErrUnknownPermission = errors.New("authz: permission not in catalog")
