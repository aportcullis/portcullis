package accessrequest

import (
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/query"
)

// List pagination (PRD §7.1): page_size is an allowlist, default 20 for any off-list value — mirrors the audit list.
const defaultPageSize = 20

var allowedPageSizes = map[int]bool{10: true, 20: true, 50: true, 100: true}

// CreateParams is the draft-creation input.
type CreateParams struct {
	ConnectionID connection.ConnectionID
	SQL          string
	Params       []query.Parameter
}

// UpdateDraftParams replaces a draft payload using an optimistic version token.
type UpdateDraftParams struct {
	SQL             string
	Params          []query.Parameter
	ExpectedVersion int64
}
