package pgdialect_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/pgdialect"
)

// Token-rebuild redaction (ADR-0016): comments dropped, literals → typed
// placeholders, $N preserved, identifiers re-quoted, keywords lowercase,
// single-space joined.
func TestRedactRewrites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		in       string
		want     string
		literals []query.LiteralType
	}{
		{
			name: "line comment dropped",
			in:   "SELECT 1 -- customer token: hunter2",
			want: "select <integer>",
			literals: []query.LiteralType{
				query.LiteralInteger,
			},
		},
		{
			name:     "block comment between tokens dropped",
			in:       "SELECT/* hunter2 */1",
			want:     "select <integer>",
			literals: []query.LiteralType{query.LiteralInteger},
		},
		{
			name:     "nested block comment dropped",
			in:       "SELECT 1 /* a /* hunter2 */ b */",
			want:     "select <integer>",
			literals: []query.LiteralType{query.LiteralInteger},
		},
		{
			name:     "string literal to typed placeholder",
			in:       "SELECT v FROM t WHERE token = 'hunter2'",
			want:     `select "v" from "t" where "token" = <string>`,
			literals: []query.LiteralType{query.LiteralString},
		},
		{
			name:     "dollar-quoted string",
			in:       "SELECT $tag$hunter2$tag$",
			want:     "select <string>",
			literals: []query.LiteralType{query.LiteralString},
		},
		{
			name:     "escape string",
			in:       `SELECT E'it\'s a secret'`,
			want:     "select <string>",
			literals: []query.LiteralType{query.LiteralString},
		},
		{
			name:     "unicode escape string",
			in:       `SELECT U&'d\0061t\+000061'`,
			want:     "select <string>",
			literals: []query.LiteralType{query.LiteralString},
		},
		{
			name: "numeric forms",
			in:   "SELECT 1e-5, 0x1F, 1_000, 1.5",
			want: "select <decimal> , <integer> , <integer> , <decimal>",
			literals: []query.LiteralType{
				query.LiteralDecimal, query.LiteralInteger, query.LiteralInteger, query.LiteralDecimal,
			},
		},
		{
			name: "bind placeholders preserved",
			in:   "SELECT * FROM t WHERE id = $1 AND v = $2",
			want: `select * from "t" where "id" = $1 and "v" = $2`,
		},
		{
			name: "typecast preserved",
			in:   "SELECT v::text FROM t",
			want: `select "v" :: text from "t"`,
		},
		{
			name: "quoted identifiers survive requoting",
			in:   `SELECT "Abc", "a""b" FROM t`,
			want: `select "Abc" , "a""b" from "t"`,
		},
		{
			name:     "comparison operators stay distinct from placeholders",
			in:       "SELECT a FROM t WHERE a < 'x' AND a > 'y'",
			want:     `select "a" from "t" where "a" < <string> and "a" > <string>`,
			literals: []query.LiteralType{query.LiteralString, query.LiteralString},
		},
		{
			name: "boolean and null keywords verbatim",
			in:   "SELECT TRUE, FALSE, NULL",
			want: "select true , false , null",
		},
		{
			name:     "bit and hex strings",
			in:       "SELECT B'1010', X'1F'",
			want:     "select <other> , <other>",
			literals: []query.LiteralType{query.LiteralOther, query.LiteralOther},
		},
		{
			name:     "trailing semicolon kept",
			in:       "SELECT 1;",
			want:     "select <integer> ;",
			literals: []query.LiteralType{query.LiteralInteger},
		},
	}
	d := pgdialect.New(pgdialect.Options{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st, err := d.ParseSingle(tt.in)
			if err != nil {
				t.Fatalf("ParseSingle(%q): %v", tt.in, err)
			}
			got, err := d.Redact(st)
			if err != nil {
				t.Fatalf("Redact(%q): %v", tt.in, err)
			}
			if got.SQL != tt.want {
				t.Fatalf("Redact(%q).SQL = %q, want %q", tt.in, got.SQL, tt.want)
			}
			if !slices.Equal(got.Literals, tt.literals) {
				t.Fatalf("Redact(%q).Literals = %v, want %v", tt.in, got.Literals, tt.literals)
			}
		})
	}
}

// Fail-closed: Redact only accepts this dialect's parse handle — malformed
// input (unterminated strings, multi-statements, comment-only) can never
// reach it because ParseSingle refuses to produce a Statement for it; a
// Statement minted elsewhere (classify_test's foreignStatement) is rejected
// with no partial output (PRD §8.4).
func TestRedactFailsClosed(t *testing.T) {
	t.Parallel()

	d := pgdialect.New(pgdialect.Options{})
	got, err := d.Redact(foreignStatement{})
	if err == nil {
		t.Fatalf("Redact(foreign statement) = %q, want error", got.SQL)
	}
	if got.SQL != "" || got.Literals != nil {
		t.Fatalf("Redact(foreign statement) returned partial output %+v with error %v", got, err)
	}
	var rej *query.Rejection
	if !errors.As(err, &rej) {
		t.Fatalf("Redact(foreign statement) error = %v, want *query.Rejection", err)
	}
}

// Property (ADR-0016): no literal byte sequence from the input survives into
// redacted output.
func TestRedactNeverLeaksLiterals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		secrets []string
	}{
		{
			in:      "SELECT * FROM t WHERE token = 'hunter2' -- key: swordfish",
			secrets: []string{"hunter2", "swordfish"},
		},
		{
			in:      "INSERT INTO t (id, v) VALUES (900123, 'p@ssw0rd!')",
			secrets: []string{"900123", "p@ssw0rd!"},
		},
		{
			in:      "SELECT $body$secret-blob$body$, X'DEADBEEF', 3.14159",
			secrets: []string{"secret-blob", "DEADBEEF", "3.14159"},
		},
		{
			in:      "UPDATE t SET v = E'esc\\'aped-secret' WHERE id = 7 /* ticket 4242 */",
			secrets: []string{"aped-secret", "4242"},
		},
	}
	d := pgdialect.New(pgdialect.Options{})
	for _, tt := range tests {
		st, err := d.ParseSingle(tt.in)
		if err != nil {
			t.Errorf("ParseSingle(%q): %v", tt.in, err)
			continue
		}
		got, err := d.Redact(st)
		if err != nil {
			t.Errorf("Redact(%q): %v", tt.in, err)
			continue
		}
		for _, secret := range tt.secrets {
			if strings.Contains(got.SQL, secret) {
				t.Errorf("Redact(%q) leaked %q: %q", tt.in, secret, got.SQL)
			}
		}
	}
}
