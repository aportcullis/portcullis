package audit

import (
	"context"

	domainaudit "github.com/aportcullis/portcullis/internal/domain/audit"
)

// EventReader reads the append-only audit trail. List returns one page ordered per
// the params; Count returns the total for the page controls (PRD §7.1). The
// postgres AuditStore satisfies it; tests substitute a fake. Consumer-defined (ISP).
type EventReader interface {
	List(ctx context.Context, p domainaudit.ListParams) ([]domainaudit.Event, error)
	Get(ctx context.Context, id string) (domainaudit.Event, error)
	Count(ctx context.Context) (int64, error)
}
