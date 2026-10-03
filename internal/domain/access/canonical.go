package access

import (
	"encoding/json"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
)

// CanonicalPayloadVersion distinguishes digest formats; v2 binds the connection config version so approvals cannot survive target replacement.
const CanonicalPayloadVersion = 2

// ApprovalUnit contains the fields authenticated by an approval digest.
type ApprovalUnit struct {
	OrganizationID identity.OrganizationID
	RequesterID    identity.UserID
	ConnectionID   connection.ConnectionID
	// ConnectionConfigVersion pins WHICH configuration of that connection was approved: host, port, database, TLS mode and credential all live behind a stable id, and replacing them is replacing the target.
	ConnectionConfigVersion int64
	PolicyVersion           int64
	Class                   connection.StatementClass
	Title                   string
	Body                    string
	SQL                     string
	Params                  []query.Parameter
}

// canonicalUnit defines the deterministic JSON shape of an approval unit.
type canonicalUnit struct {
	PayloadVersion          int              `json:"payload_version"`
	OrganizationID          string           `json:"organization_id"`
	RequesterID             string           `json:"requester_id"`
	ConnectionID            string           `json:"connection_id"`
	ConnectionConfigVersion int64            `json:"connection_config_version"`
	PolicyVersion           int64            `json:"policy_version"`
	StatementClass          string           `json:"statement_class"`
	SQL                     string           `json:"sql"`
	Params                  []canonicalParam `json:"params"`
	Title                   string           `json:"title,omitempty"`
	Body                    string           `json:"body,omitempty"`
}

type canonicalParam struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

// CanonicalPayload authenticates narrative, normalized SQL and sorted parameters without changing empty-narrative legacy digests.
func CanonicalPayload(u ApprovalUnit) ([]byte, error) {
	ps := make([]canonicalParam, len(u.Params))
	for idx, p := range u.Params {
		ps[idx] = canonicalParam{Name: p.Name, Type: string(p.Value.Type), Value: p.Value.Text}
	}
	sort.Slice(ps, func(leftIdx, rightIdx int) bool { return ps[leftIdx].Name < ps[rightIdx].Name })
	return json.Marshal(canonicalUnit{
		PayloadVersion:          CanonicalPayloadVersion,
		OrganizationID:          string(u.OrganizationID),
		RequesterID:             string(u.RequesterID),
		ConnectionID:            string(u.ConnectionID),
		ConnectionConfigVersion: u.ConnectionConfigVersion,
		PolicyVersion:           u.PolicyVersion,
		StatementClass:          string(u.Class),
		SQL:                     normalizeSQLForDigest(u.SQL),
		Params:                  ps,
		Title:                   u.Title,
		Body:                    u.Body,
	})
}

// normalizeSQLForDigest normalizes line endings and Unicode to NFC without rewriting SQL.
func normalizeSQLForDigest(sql string) string {
	s := strings.ReplaceAll(sql, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return norm.NFC.String(s)
}
