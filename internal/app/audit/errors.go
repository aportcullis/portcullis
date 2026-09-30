package audit

import "errors"

// ErrInvalidSortField is returned when a page request names a sort column that is not in the whitelist (PRD §7.1). The transport maps it to InvalidArgument.
var ErrInvalidSortField = errors.New("audit: sort field not allowed")
