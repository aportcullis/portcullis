package audit

import (
	"context"

	domainaudit "github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// EventReader returns rows, total, and page clamp from one snapshot within the caller's organization; separate Count calls are excluded (PRD §7.1, ADR-0004).
type EventReader interface {
	// DefaultOrganizationID resolves the single self-hosted organization (single-org MVP, ADR-0004).
	DefaultOrganizationID(ctx context.Context) (identity.OrganizationID, error)
	List(ctx context.Context, org identity.OrganizationID, p domainaudit.ListParams) (domainaudit.EventPage, error)
	Get(ctx context.Context, org identity.OrganizationID, id string) (domainaudit.Event, error)
}
