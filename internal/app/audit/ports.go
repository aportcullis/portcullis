package audit

import (
	"context"

	domainaudit "github.com/aportcullis/portcullis/internal/domain/audit"
)

// EventReader returns rows, total, and page clamp from one snapshot; separate Count calls are excluded (PRD §7.1).
type EventReader interface {
	List(ctx context.Context, p domainaudit.ListParams) (domainaudit.EventPage, error)
	Get(ctx context.Context, id string) (domainaudit.Event, error)
}
