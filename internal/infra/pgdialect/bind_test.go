package pgdialect_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/pgdialect"
)

func param(name string, typ query.ParamType, text string) query.Parameter {
	return query.Parameter{Name: name, Value: query.TypedValue{Type: typ, Text: text}}
}

// BindNamed replaces :name with $N in first-appearance order, splicing the
// original bytes everywhere else (ADR-0016).
func TestBindNamedSubstitutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sql       string
		params    []query.Parameter
		wantSQL   string
		wantTypes []query.ParamType
	}{
		{
			name:      "two parameters",
			sql:       "SELECT * FROM t WHERE id = :id AND v = :v",
			params:    []query.Parameter{param("id", query.ParamInteger, "1"), param("v", query.ParamString, "x")},
			wantSQL:   "SELECT * FROM t WHERE id = $1 AND v = $2",
			wantTypes: []query.ParamType{query.ParamInteger, query.ParamString},
		},
		{
			name:      "repeated name reuses its number",
			sql:       "SELECT :a + :b + :a",
			params:    []query.Parameter{param("a", query.ParamInteger, "1"), param("b", query.ParamInteger, "2")},
			wantSQL:   "SELECT $1 + $2 + $1",
			wantTypes: []query.ParamType{query.ParamInteger, query.ParamInteger},
		},
		{
			name:      "numbering follows first appearance, not the params slice",
			sql:       "SELECT :b, :a",
			params:    []query.Parameter{param("a", query.ParamString, "va"), param("b", query.ParamString, "vb")},
			wantSQL:   "SELECT $1, $2",
			wantTypes: []query.ParamType{query.ParamString, query.ParamString},
		},
		{
			name:      "colon inside string literal untouched",
			sql:       "SELECT ':id', id FROM t WHERE id = :id",
			params:    []query.Parameter{param("id", query.ParamInteger, "1")},
			wantSQL:   "SELECT ':id', id FROM t WHERE id = $1",
			wantTypes: []query.ParamType{query.ParamInteger},
		},
		{
			name:      "colon inside comments untouched",
			sql:       "SELECT id /* :id */ FROM t WHERE id = :id -- :id",
			params:    []query.Parameter{param("id", query.ParamInteger, "1")},
			wantSQL:   "SELECT id /* :id */ FROM t WHERE id = $1 -- :id",
			wantTypes: []query.ParamType{query.ParamInteger},
		},
		{
			name:      "keyword-shaped parameter name",
			sql:       "SELECT * FROM t LIMIT :limit",
			params:    []query.Parameter{param("limit", query.ParamInteger, "10")},
			wantSQL:   "SELECT * FROM t LIMIT $1",
			wantTypes: []query.ParamType{query.ParamInteger},
		},
		{
			name:      "typecast not confused with parameter",
			sql:       "SELECT v::text FROM t WHERE v = :v",
			params:    []query.Parameter{param("v", query.ParamString, "x")},
			wantSQL:   "SELECT v::text FROM t WHERE v = $1",
			wantTypes: []query.ParamType{query.ParamString},
		},
		{
			name:    "array slice untouched without parameters",
			sql:     "SELECT arr[1:2] FROM t",
			wantSQL: "SELECT arr[1:2] FROM t",
		},
		{
			name:      "spaced slice colon never a parameter",
			sql:       "SELECT arr[i : j] FROM t WHERE k = :j",
			params:    []query.Parameter{param("j", query.ParamInteger, "3")},
			wantSQL:   "SELECT arr[i : j] FROM t WHERE k = $1",
			wantTypes: []query.ParamType{query.ParamInteger},
		},
		{
			name:    "no parameters validates and passes through",
			sql:     "SELECT 1",
			wantSQL: "SELECT 1",
		},
		{
			name:    "omitted-lower-bound slice colon is not a parameter",
			sql:     "SELECT arr[:hi] FROM t",
			wantSQL: "SELECT arr[:hi] FROM t",
		},
		{
			name:    "spaced omitted-lower-bound slice colon is not a parameter",
			sql:     "SELECT arr[ :hi ] FROM t",
			wantSQL: "SELECT arr[ :hi ] FROM t",
		},
		{
			name:    "parenthesized slice lower bound is not a parameter",
			sql:     "SELECT arr[(i):j] FROM t",
			wantSQL: "SELECT arr[(i):j] FROM t",
		},
		{
			name:    "function-call slice lower bound is not a parameter",
			sql:     "SELECT arr[fn(i):j] FROM t",
			wantSQL: "SELECT arr[fn(i):j] FROM t",
		},
		{
			name:    "space before the slice colon is not a parameter",
			sql:     "SELECT arr[i :j] FROM t",
			wantSQL: "SELECT arr[i :j] FROM t",
		},
		{
			name:      "parentheses bind inside a subscript",
			sql:       "SELECT arr[(:x)] FROM t",
			params:    []query.Parameter{param("x", query.ParamInteger, "1")},
			wantSQL:   "SELECT arr[($1)] FROM t",
			wantTypes: []query.ParamType{query.ParamInteger},
		},
		{
			name:      "surrounding bytes spliced exactly",
			sql:       "SELECT  /*c*/ *  FROM t WHERE a=:a AND b=:b",
			params:    []query.Parameter{param("a", query.ParamInteger, "1"), param("b", query.ParamInteger, "2")},
			wantSQL:   "SELECT  /*c*/ *  FROM t WHERE a=$1 AND b=$2",
			wantTypes: []query.ParamType{query.ParamInteger, query.ParamInteger},
		},
	}
	d := pgdialect.New(pgdialect.Options{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bound, args, err := d.BindNamed(tt.sql, tt.params)
			if err != nil {
				t.Fatalf("BindNamed(%q): %v", tt.sql, err)
			}
			if bound != tt.wantSQL {
				t.Fatalf("bound = %q, want %q", bound, tt.wantSQL)
			}
			var types []query.ParamType
			for _, a := range args {
				types = append(types, a.Type)
			}
			if !slices.Equal(types, tt.wantTypes) {
				t.Fatalf("arg types = %v, want %v", types, tt.wantTypes)
			}
		})
	}
}

// Every misuse fails closed with its dedicated sentinel.
func TestBindNamedErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sql     string
		params  []query.Parameter
		wantErr error
	}{
		{
			name:    "unknown parameter in SQL",
			sql:     "SELECT * FROM t WHERE id = :missing",
			wantErr: query.ErrUnknownParameter,
		},
		{
			name:    "provided parameter unused",
			sql:     "SELECT 1",
			params:  []query.Parameter{param("x", query.ParamInteger, "1")},
			wantErr: query.ErrUnusedParameter,
		},
		{
			name:    "positional parameter rejected",
			sql:     "SELECT * FROM t WHERE id = $1",
			wantErr: query.ErrPositionalParams,
		},
		{
			name:    "invalid parameter value",
			sql:     "SELECT :x",
			params:  []query.Parameter{param("x", query.ParamInteger, "abc")},
			wantErr: query.ErrInvalidParamValue,
		},
		{
			name:    "malformed parameter name",
			sql:     "SELECT 1",
			params:  []query.Parameter{param("1bad", query.ParamInteger, "1")},
			wantErr: query.ErrInvalidParamName,
		},
		{
			name:    "dollar in parameter name rejected",
			sql:     "SELECT 1",
			params:  []query.Parameter{param("bad$name", query.ParamInteger, "1")},
			wantErr: query.ErrInvalidParamName,
		},
		{
			name: "duplicate parameter name",
			sql:  "SELECT :x",
			params: []query.Parameter{
				param("x", query.ParamInteger, "1"),
				param("x", query.ParamInteger, "2"),
			},
			wantErr: query.ErrInvalidParamName,
		},
		{
			name:    "glued colon is not a parameter so it goes unused",
			sql:     "SELECT col:lim FROM t",
			params:  []query.Parameter{param("lim", query.ParamInteger, "1")},
			wantErr: query.ErrUnusedParameter,
		},
		{
			name: "slice colon after identifier never captures",
			sql:  "SELECT arr[i:j] FROM t",
			params: []query.Parameter{
				param("i", query.ParamInteger, "1"),
				param("j", query.ParamInteger, "2"),
			},
			wantErr: query.ErrUnusedParameter,
		},
		{
			name:    "paren slice upper bound never captures even with a matching name",
			sql:     "SELECT arr[(i):j] FROM t",
			params:  []query.Parameter{param("j", query.ParamInteger, "2")},
			wantErr: query.ErrUnusedParameter,
		},
		{
			name:    "spaced colon is not a parameter",
			sql:     "SELECT id FROM t WHERE id = : id",
			params:  []query.Parameter{param("id", query.ParamInteger, "1")},
			wantErr: query.ErrUnusedParameter,
		},
	}
	d := pgdialect.New(pgdialect.Options{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := d.BindNamed(tt.sql, tt.params)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("BindNamed(%q) error = %v, want %v", tt.sql, err, tt.wantErr)
			}
		})
	}
}

// The unknown-parameter error names the parameter (names are part of the SQL,
// not sensitive) but never the parameter values.
func TestBindNamedErrorHygiene(t *testing.T) {
	t.Parallel()
	d := pgdialect.New(pgdialect.Options{})
	_, _, err := d.BindNamed("SELECT :x", []query.Parameter{param("x", query.ParamDate, "hunter2-not-a-date")})
	if !errors.Is(err, query.ErrInvalidParamValue) {
		t.Fatalf("err = %v, want ErrInvalidParamValue", err)
	}
	if s := err.Error(); strings.Contains(s, "hunter2") {
		t.Fatalf("error %q leaks the parameter value", s)
	}
}
