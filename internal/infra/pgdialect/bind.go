package pgdialect

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/pgplex/pgparser/parser"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

// maxParamNameLen bounds a :name (ADR-0016).
const maxParamNameLen = 64

var errBindLexFailure = errors.New("pgdialect: parameter scan lexing failed")

// BindNamed replaces :name outside literals and comments, preserving other bytes and first-use argument order. Validate unknown, unused, malformed, and positional parameters before parsing (ADR-0016).
func (d *Dialect) BindNamed(sql string, params []query.Parameter) (string, []query.TypedValue, error) {
	values := make(map[string]query.TypedValue, len(params))
	for _, p := range params {
		name := strings.ToLower(p.Name)
		if !validParamName(name) {
			return "", nil, fmt.Errorf("parameter %q: %w", p.Name, query.ErrInvalidParamName)
		}
		if _, dup := values[name]; dup {
			return "", nil, fmt.Errorf("parameter %q: %w", p.Name, query.ErrInvalidParamName)
		}
		if err := p.Value.Validate(); err != nil {
			return "", nil, fmt.Errorf("parameter %q: %w", p.Name, err)
		}
		values[name] = p.Value
	}

	type span struct {
		start, end int // byte span of ":name" in sql
		number     int // assigned $N
	}
	var (
		spans   []span
		numbers = make(map[string]int, len(values))
		args    []query.TypedValue
	)

	codes, err := lexCodes()
	if err != nil {
		return "", nil, err
	}
	lexer := parser.NewLexer(sql)
	// The previous token, for slice-context detection.
	prevStr, prevType := "", 0
	for {
		tok := lexer.NextToken()
		if tok.Type == 0 { // EOF
			break
		}
		if tok.Type == codes.param {
			// A real positional parameter token ($N outside strings/comments).
			return "", nil, query.ErrPositionalParams
		}
		start := tok.Loc
		// Treat :name as a bind parameter unless the preceding token makes it an array-slice bound. Bind inside a subscript with parentheses: arr[(:x)] (ADR-0016).
		endsSubscriptBound := prevStr == "[" || prevStr == ")" || prevStr == "]" ||
			codes.ident[prevType] || codes.literal[prevType] != ""
		isParam := tok.Str == ":" && sql[tok.Loc] == ':' &&
			start+1 < len(sql) && identStart(sql[start+1]) &&
			!endsSubscriptBound
		if isParam {
			end := start + 1
			for end < len(sql) && identCont(sql[end]) {
				end++
			}
			name := strings.ToLower(sql[start+1 : end])
			value, known := values[name]
			if !known {
				return "", nil, fmt.Errorf("parameter %q: %w", name, query.ErrUnknownParameter)
			}
			number, seen := numbers[name]
			if !seen {
				number = len(numbers) + 1
				numbers[name] = number
				args = append(args, value)
			}
			spans = append(spans, span{start: start, end: end, number: number})
		}
		prevStr, prevType = tok.Str, tok.Type
	}
	if lexer.Err != nil {
		return "", nil, errBindLexFailure
	}

	for name := range values {
		if _, used := numbers[name]; !used {
			return "", nil, fmt.Errorf("parameter %q: %w", name, query.ErrUnusedParameter)
		}
	}

	var out strings.Builder
	prev := 0
	for _, s := range spans {
		out.WriteString(sql[prev:s.start])
		out.WriteString("$" + strconv.Itoa(s.number))
		prev = s.end
	}
	out.WriteString(sql[prev:])
	return out.String(), args, nil
}

// validParamName enforces the ADR-0016 named-parameter charset [A-Za-z_][A-Za-z0-9_]*, ≤ maxParamNameLen. It is deliberately stricter than identCont, which also admits '$' (a PostgreSQL identifier character): a parameter name is a narrower vocabulary than a PG identifier.
func validParamName(name string) bool {
	if name == "" || len(name) > maxParamNameLen || !identStart(name[0]) {
		return false
	}
	for idx := 1; idx < len(name); idx++ {
		c := name[idx]
		if !identStart(c) && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func identStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func identCont(c byte) bool {
	return identStart(c) || (c >= '0' && c <= '9') || c == '$'
}
