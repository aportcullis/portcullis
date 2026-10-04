package postgres_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Mutation stamps use post-lock observed time; enumerate justified exceptions to the now() prohibition (ADR-0009).
var nowAllowedInWrites = map[string]string{
	"RecordLoginFailure": "backoff arithmetic, not a stamp: locked_until is a deadline computed from server time, " +
		"and the login path takes no row lock that could park it behind another writer",
	"UpsertPasswordAuth": "credential rows have no lock-wait path and no audit event ordering against them",
	"RevokeSession":      "session revocation is uncontended and its trail is the auth event, not revoked_at",
	"RevokeUserSessions": "same as RevokeSession, for the fan-out on a role change",
	"InsertAuditEvent":   "the fallback for events whose transaction observed no instant; stamped callers pass occurred_at",
	"InsertAuditEvents":  "the batched form of InsertAuditEvent with the same fallback",
}

var eventTimeColumns = []string{
	"created_at", "updated_at", "submitted_at", "decided_at", "occurred_at", "archived_at",
}

// Use @at for caller-observed time or stamped/observed CTEs for database observations; other sources may hide application-clock stamps.
var observedInstantSources = []string{
	"stamped.at", "observed.at", "clock_timestamp()",
	"(select at from stamped)", "(select at from observed)",
}

var observedInstantParam = regexp.MustCompile(`(?i)^\s*(@at\b|sqlc\.arg\(\s*at\s*\))`)

var callerEventTimeAllowed = map[string]string{
	"InsertAuditEvent": "the narg fallback: a caller that observed an instant passes occurred_at, and one that " +
		"did not falls back to the column default — the fallback itself is covered by nowAllowedInWrites",
	"InsertAuditEvents": "the batched form of InsertAuditEvent with the same narg fallback",
}

var (
	queryName  = regexp.MustCompile(`(?m)^--\s*name:\s*(\w+)`)
	writeStart = regexp.MustCompile(`(?is)\b(insert\s+into|update\s+public\.|delete\s+from)\b`)
)

func TestWriteQueriesDoNotStampWithNow(t *testing.T) {
	dir := "queries"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read queries: %v", err)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for name, sql := range namedStatements(string(body)) {
			seen[name] = true
			if !writeStart.MatchString(sql) || !strings.Contains(sql, "now()") {
				continue
			}
			if _, allowed := nowAllowedInWrites[name]; allowed {
				continue
			}
			t.Errorf("%s (%s) stamps with now(): a write must use the instant its caller observed after "+
				"taking the lock, or be added to nowAllowedInWrites with a reason (ADR-0009)", name, e.Name())
		}
	}

	for name := range nowAllowedInWrites {
		if !seen[name] {
			t.Errorf("nowAllowedInWrites lists %q, which no query defines any more — drop the exception", name)
		}
	}
}

func TestWriteQueriesTakeEventTimesFromTheObservedInstant(t *testing.T) {
	dir := "queries"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read queries: %v", err)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for name, sql := range namedStatements(string(body)) {
			seen[name] = true
			if !writeStart.MatchString(sql) {
				continue
			}
			if _, allowed := callerEventTimeAllowed[name]; allowed {
				continue
			}
			for _, column := range eventTimeColumns {
				if !writesEventTimeFromCaller(sql, column) {
					continue
				}
				t.Errorf("%s (%s) writes %s from the caller instead of the instant this transaction observed: "+
					"an event time says WHEN this happened, so it may not come from the application's clock "+
					"(ADR-0009). Deadlines like expires_at answer 'until when' and are not covered by this rule.",
					name, e.Name(), column)
			}
		}
	}
	for name := range callerEventTimeAllowed {
		if !seen[name] {
			t.Errorf("callerEventTimeAllowed lists %q, which no query defines any more — drop the exception", name)
		}
	}
}

// writesEventTimeFromCaller checks UPDATE assignments and INSERT timestamp columns for caller parameters, ignoring WHERE predicates and SQL-generated timestamps.
func writesEventTimeFromCaller(sql, column string) bool {
	if assignments := updateAssignments(sql); assignments != "" {
		assigned := regexp.MustCompile(`(?is)\b` + regexp.QuoteMeta(column) +
			`\s*=\s*(@\w+|\$\d+|sqlc\.(arg|narg)\([^)]*\))`).FindStringSubmatch(assignments)
		return assigned != nil && !observedInstantParam.MatchString(assigned[1])
	}
	columns := insertColumnList(sql)
	if columns == "" {
		return false
	}
	if !regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(column) + `\b`).MatchString(columns) {
		return false
	}
	return !readsObservedInstant(sql)
}

// readsObservedInstant reports whether the statement gets the transaction's instant at all — as the `@at` parameter a locking store passes, or by reading the clock itself.
func readsObservedInstant(sql string) bool {
	lowered := strings.ToLower(sql)
	for _, source := range observedInstantSources {
		if strings.Contains(lowered, source) {
			return true
		}
	}
	return regexp.MustCompile(`(?i)@at\b|sqlc\.arg\(\s*at\s*\)`).MatchString(sql)
}

var (
	updateSet    = regexp.MustCompile(`(?is)\bset\b(.*?)(\bwhere\b|\breturning\b|;)`)
	insertColumn = regexp.MustCompile(`(?is)\binsert\s+into\s+[\w.]+\s*\((.*?)\)\s*values`)
)

var (
	relationReference = regexp.MustCompile(`(?i)\b(from|join|into|update)\s+([a-z_][a-z0-9_]*(?:\.[a-z_][a-z0-9_]*)?)\b`)
	commonTableName   = regexp.MustCompile(`(?i)\b([a-z_][a-z0-9_]*)\s+as\s+(?:not\s+)?(?:materialized\s+)?\(`)
	extractField      = regexp.MustCompile(`(?i)\bextract\s*\(\s*[a-z]+\s+from\b`)
)

var relationClauseWords = map[string]bool{"set": true, "skip": true, "nowait": true, "of": true}

// unqualifiedRelations lists relation names a statement reads or writes without a schema, ignoring its own CTE names.
func unqualifiedRelations(sql string) []string {
	sql = extractField.ReplaceAllString(sql, "extract(field ")
	local := map[string]bool{}
	for _, match := range commonTableName.FindAllStringSubmatch(sql, -1) {
		local[strings.ToLower(match[1])] = true
	}
	var unqualified []string
	for _, match := range relationReference.FindAllStringSubmatch(sql, -1) {
		relation := strings.ToLower(match[2])
		// "do update set" and "for update skip locked/nowait/of" are clauses, not relations.
		if strings.Contains(relation, ".") || local[relation] || relationClauseWords[relation] {
			continue
		}
		unqualified = append(unqualified, relation)
	}
	return unqualified
}

func TestQueriesQualifyMetadataRelations(t *testing.T) {
	entries, err := os.ReadDir("queries")
	if err != nil {
		t.Fatalf("read queries: %v", err)
	}
	checked := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		body, err := os.ReadFile(filepath.Join("queries", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for name, sql := range namedStatements(string(body)) {
			checked++
			for _, relation := range unqualifiedRelations(sql) {
				t.Errorf("%s (%s) references %q without a schema; qualify metadata relations with public. so a temporary or search_path relation cannot shadow them (data.md)", name, entry.Name(), relation)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no named queries were checked")
	}
}

func TestUnqualifiedRelationDetector(t *testing.T) {
	for _, tc := range []struct {
		sql  string
		want []string
	}{
		{`select * from public.users u join public.roles r on true`, nil},
		{`with stamped as materialized (select clock_timestamp() as at) insert into public.sessions select 1 from stamped`, nil},
		{`select extract(epoch from expires_at) from result_cache.result_sets`, nil},
		{`with observed as (select clock_timestamp() as at) update public.access_requests set updated_at = (select at from observed)`, nil},
		{`insert into public.auth_methods (id) values (1) on conflict (id) do update set secret = excluded.secret`, nil},
		{`select id from result_cache.result_sets limit 1 for update skip locked`, nil},
		{`select * from connection_policy_versions p`, []string{"connection_policy_versions"}},
		{`insert into audit_events (id) values (1)`, []string{"audit_events"}},
		{`update sessions set revoked_at = now()`, []string{"sessions"}},
		{`select 1 from public.connections c join organizations o on true`, []string{"organizations"}},
	} {
		got := unqualifiedRelations(tc.sql)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("unqualifiedRelations(%q) = %v, want %v", tc.sql, got, tc.want)
		}
	}
}

func updateAssignments(sql string) string {
	m := updateSet.FindStringSubmatch(sql)
	if m == nil {
		return ""
	}
	return m[1]
}

func insertColumnList(sql string) string {
	m := insertColumn.FindStringSubmatch(sql)
	if m == nil {
		return ""
	}
	return m[1]
}

func namedStatements(file string) map[string]string {
	out := map[string]string{}
	matches := queryName.FindAllStringSubmatchIndex(file, -1)
	for idx, m := range matches {
		end := len(file)
		if idx+1 < len(matches) {
			end = matches[idx+1][0]
		}
		var sql strings.Builder
		for _, line := range strings.Split(file[m[1]:end], "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "--") {
				continue
			}
			sql.WriteString(line)
			sql.WriteString("\n")
		}
		out[file[m[2]:m[3]]] = sql.String()
	}
	return out
}
