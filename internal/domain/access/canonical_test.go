package access_test

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
)

func canonical(t *testing.T, org, req, conn string, ver int64, class connection.StatementClass, sql string, params ...query.Parameter) []byte {
	t.Helper()
	b, err := access.CanonicalPayload(access.ApprovalUnit{
		OrganizationID:          identity.OrganizationID("org-" + org),
		RequesterID:             identity.UserID("user-" + req),
		ConnectionID:            connection.ConnectionID("conn-" + conn),
		ConnectionConfigVersion: 1,
		PolicyVersion:           ver,
		Class:                   class,
		SQL:                     sql,
		Params:                  params,
	})
	if err != nil {
		t.Fatalf("CanonicalPayload: %v", err)
	}
	return b
}

func TestCanonicalPayloadBindsEveryPart(t *testing.T) {
	t.Parallel()
	base := canonical(t, "1", "1", "1", 3, connection.ClassRead, "select 1")

	cases := map[string][]byte{
		"different connection": canonical(t, "1", "1", "2", 3, connection.ClassRead, "select 1"),
		"different requester":  canonical(t, "1", "2", "1", 3, connection.ClassRead, "select 1"),
		"different org":        canonical(t, "2", "1", "1", 3, connection.ClassRead, "select 1"),
		"different policy ver": canonical(t, "1", "1", "1", 4, connection.ClassRead, "select 1"),
		"different class":      canonical(t, "1", "1", "1", 3, connection.ClassWrite, "select 1"),
		"different param value": canonical(t, "1", "1", "1", 3, connection.ClassRead, "select :id",
			query.Parameter{Name: "id", Value: query.TypedValue{Type: query.ParamInteger, Text: "7"}}),
	}
	for name, other := range cases {
		if bytes.Equal(base, other) {
			t.Errorf("%s: canonical bytes must differ from the base unit", name)
		}
	}
}

func TestCanonicalPayloadBindsTheConnectionConfig(t *testing.T) {
	t.Parallel()
	unit := access.ApprovalUnit{
		OrganizationID:          "org-1",
		RequesterID:             "user-1",
		ConnectionID:            "conn-1",
		ConnectionConfigVersion: 1,
		PolicyVersion:           3,
		Class:                   connection.ClassRead,
		SQL:                     "select 1",
	}
	base, err := access.CanonicalPayload(unit)
	if err != nil {
		t.Fatalf("CanonicalPayload: %v", err)
	}
	replaced := unit
	replaced.ConnectionConfigVersion = 2
	after, err := access.CanonicalPayload(replaced)
	if err != nil {
		t.Fatalf("CanonicalPayload: %v", err)
	}
	if bytes.Equal(base, after) {
		t.Error("a config replacement must change the canonical bytes — the approval was for the OLD target")
	}
}

func TestCanonicalPayloadFieldSetIsPinned(t *testing.T) {
	t.Parallel()
	b, err := access.CanonicalPayload(access.ApprovalUnit{
		OrganizationID: "org-1", RequesterID: "user-1", ConnectionID: "conn-1",
		ConnectionConfigVersion: 1, PolicyVersion: 3, Class: connection.ClassRead, SQL: "select 1",
	})
	if err != nil {
		t.Fatalf("CanonicalPayload: %v", err)
	}
	var unit map[string]json.RawMessage
	if err := json.Unmarshal(b, &unit); err != nil {
		t.Fatalf("canonical bytes are not an object: %v", err)
	}
	want := []string{
		"payload_version", "organization_id", "requester_id", "connection_id",
		"connection_config_version", "policy_version", "statement_class", "sql", "params",
	}
	got := make([]string, 0, len(unit))
	for key := range unit {
		got = append(got, key)
	}
	sort.Strings(got)
	sorted := append([]string(nil), want...)
	sort.Strings(sorted)
	if strings.Join(got, ",") != strings.Join(sorted, ",") {
		t.Errorf("approval unit fields = %v, want exactly %v — changing the unit means deciding what happens to "+
			"CanonicalPayloadVersion and to every digest already stored", got, sorted)
	}
	if access.CanonicalPayloadVersion != 2 {
		t.Errorf("CanonicalPayloadVersion = %d, want 2 — the unit gained connection_config_version",
			access.CanonicalPayloadVersion)
	}
}

func TestCanonicalPayloadDeterministic(t *testing.T) {
	t.Parallel()
	p := []query.Parameter{
		{Name: "b", Value: query.TypedValue{Type: query.ParamString, Text: "y"}},
		{Name: "a", Value: query.TypedValue{Type: query.ParamInteger, Text: "1"}},
	}

	one := canonical(t, "1", "1", "1", 3, connection.ClassRead, "select :a, :b", p...)
	reordered := []query.Parameter{p[1], p[0]}
	two := canonical(t, "1", "1", "1", 3, connection.ClassRead, "select :a, :b", reordered...)
	if !bytes.Equal(one, two) {
		t.Error("param order must not change the canonical bytes")
	}
}

func TestCanonicalPayloadNormalizesLineEndings(t *testing.T) {
	t.Parallel()
	lf := canonical(t, "1", "1", "1", 3, connection.ClassRead, "select 1\nwhere x")
	crlf := canonical(t, "1", "1", "1", 3, connection.ClassRead, "select 1\r\nwhere x")
	if !bytes.Equal(lf, crlf) {
		t.Error("CRLF and LF forms must normalize to the same bytes")
	}
}
