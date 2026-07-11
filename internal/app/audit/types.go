package audit

// Service is the read side of the audit trail: it validates a page request against
// the PRD §7.1 policy (page-size whitelist, sort-column whitelist), delegates to the
// EventReader, and assembles the page with its totals. Its constructor and methods
// live in service.go (file-split convention). Emission of events lives in the auth
// service and the identity store; this package only reads.
type Service struct {
	reader EventReader
}

// Query is a page request for the audit list (PRD §7.1). Page is 1-based; a value
// below 1 becomes page 1. PageSize outside {10,20,50,100} falls back to 20.
// SortField "" means the default column (occurred_at); any other value must be in
// the whitelist or List returns ErrInvalidSortField. SortAscending defaults to
// false, i.e. newest-first — the natural order for a log.
type Query struct {
	Page          int
	PageSize      int
	SortField     string
	SortAscending bool
}
