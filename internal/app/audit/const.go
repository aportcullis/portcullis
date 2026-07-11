package audit

// Page-size policy (PRD §7.1): the size must be one of a fixed whitelist, defaults
// to 20, and is capped at 100. A request value outside the set falls back to the
// default rather than being clamped, so the client always gets a known page size.
const defaultPageSize = 20

var allowedPageSizes = map[int]bool{10: true, 20: true, 50: true, 100: true}

// Sortable columns (PRD §7.1 column whitelist). Audit is a newest-first log, so
// occurred_at is the only meaningful axis today; the whitelist rejects any other
// column and makes adding one a two-line change (extend the map + the store's
// direction/column switch).
const sortByOccurredAt = "occurred_at"

var allowedSortFields = map[string]bool{sortByOccurredAt: true}
