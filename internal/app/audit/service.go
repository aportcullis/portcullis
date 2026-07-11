// Package audit is the application service for reading the audit trail. It holds no
// storage or transport detail: it validates a page request against the PRD §7.1
// policy, delegates to an injected EventReader (ports.go), and assembles the page
// with its totals. The write side (event emission) lives in the auth service and
// the identity store.
package audit

import (
	"context"
	"errors"

	domainaudit "github.com/aportcullis/portcullis/internal/domain/audit"
)

// New builds the read service over an EventReader. It fails if the reader is nil,
// so a half-built service never starts.
func New(reader EventReader) (*Service, error) {
	if reader == nil {
		return nil, errors.New("audit: nil event reader")
	}
	return &Service{reader: reader}, nil
}

// List returns one page of audit events plus the totals for explicit page controls
// (PRD §7.1). It normalizes the request (page ≥ 1, page size to the whitelist/
// default, sort column validated) and computes the OFFSET, then reads the count and
// the page. Offsets are int64, so even the largest page can never wrap negative.
func (s *Service) List(ctx context.Context, q Query) (domainaudit.EventPage, error) {
	page := q.Page
	if page < 1 {
		page = 1
	}
	pageSize := q.PageSize
	if !allowedPageSizes[pageSize] {
		pageSize = defaultPageSize
	}
	if q.SortField != "" && !allowedSortFields[q.SortField] {
		return domainaudit.EventPage{}, ErrInvalidSortField
	}

	total, err := s.reader.Count(ctx)
	if err != nil {
		return domainaudit.EventPage{}, err
	}
	offset := int64(page-1) * int64(pageSize)
	events, err := s.reader.List(ctx, domainaudit.ListParams{
		Limit:          int64(pageSize),
		Offset:         offset,
		SortDescending: !q.SortAscending,
	})
	if err != nil {
		return domainaudit.EventPage{}, err
	}
	return domainaudit.EventPage{
		Events:     events,
		Page:       page,
		PageSize:   pageSize,
		TotalCount: total,
		TotalPages: totalPages(total, pageSize),
	}, nil
}

// totalPages is ceil(total / pageSize), or 0 when there are no rows.
func totalPages(total int64, pageSize int) int {
	if total <= 0 {
		return 0
	}
	return int((total + int64(pageSize) - 1) / int64(pageSize))
}
