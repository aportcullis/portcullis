package postgres

// authorizedHistoryPage retains the requested page and a last-page fallback while counting visible rows.
type authorizedHistoryPage[Row any] struct {
	requested int
	size      int
	count     int
	selected  []Row
	last      []Row
}

// appendVisible counts only authorized rows and bounds retained display data independently of history size.
func (page *authorizedHistoryPage[Row]) appendVisible(row Row) {
	index := page.count
	page.count++
	if index/page.size == max(page.requested, 1)-1 {
		page.selected = append(page.selected, row)
	}
	if index%page.size == 0 {
		page.last = page.last[:0]
	}
	page.last = append(page.last, row)
}

// result clamps only after the complete authorized total is known.
func (page *authorizedHistoryPage[Row]) result() ([]Row, int) {
	number, _, _ := authorizedPageBounds(page.requested, page.size, page.count)
	if number != max(page.requested, 1) {
		return page.last, number
	}
	return page.selected, number
}
