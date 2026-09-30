package audit

// Service validates audit page requests and delegates snapshot reads; event emission belongs to mutation services.
type Service struct {
	reader EventReader
}

// Query defaults to page 1, size 20, and newest-first. Allowed sizes are 10/20/50/100; unknown sort fields are refused (PRD §7.1).
type Query struct {
	Page          int
	PageSize      int
	SortField     string
	SortAscending bool
}
