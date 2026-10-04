// Package audit validates and reads paged audit evidence through an injected EventReader.
package audit

import (
	"context"
	"errors"

	domainaudit "github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// New builds the read service over an EventReader. It fails if the reader is nil, so a half-built service never starts.
func New(reader EventReader) (*Service, error) {
	if reader == nil {
		return nil, errors.New("audit: nil event reader")
	}
	return &Service{reader: reader}, nil
}

// List validates pagination and sorting, then delegates rows, count, and page clamp to one repository snapshot (PRD §7.1).
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

	org, err := s.resolveOrganization(ctx)
	if err != nil {
		return domainaudit.EventPage{}, err
	}
	got, err := s.reader.List(ctx, org, domainaudit.ListParams{
		Page:           page,
		PageSize:       pageSize,
		SortDescending: !q.SortAscending,
	})
	if err != nil {
		return domainaudit.EventPage{}, err
	}
	got.PageSize = pageSize
	got.TotalPages = totalPages(got.TotalCount, pageSize)
	return got, nil
}

// Get returns one event's full detail. Authorization belongs at the transport boundary; this use case preserves the repository's org-scoped not-found result without exposing storage details.
func (s *Service) Get(ctx context.Context, id string) (domainaudit.Event, error) {
	org, err := s.resolveOrganization(ctx)
	if err != nil {
		return domainaudit.Event{}, err
	}
	return s.reader.Get(ctx, org, id)
}

// resolveOrganization returns the caller's organization and refuses an empty scope.
func (s *Service) resolveOrganization(ctx context.Context) (identity.OrganizationID, error) {
	org, err := s.reader.DefaultOrganizationID(ctx)
	if err != nil {
		return "", err
	}
	if org == "" {
		return "", domainaudit.ErrOrganizationRequired
	}
	return org, nil
}

// totalPages is ceil(total / pageSize), or 0 when there are no rows.
func totalPages(total int64, pageSize int) int {
	if total <= 0 {
		return 0
	}
	return int((total + int64(pageSize) - 1) / int64(pageSize))
}
