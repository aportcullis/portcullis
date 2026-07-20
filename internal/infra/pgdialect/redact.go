package pgdialect

import (
	"errors"
	"strconv"
	"strings"
	"sync"

	"github.com/pgplex/pgparser/parser"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

// Redact rebuilds the bound single statement from its token stream: comments
// never reach the stream (the lexer skips them), inline literals become
// typed placeholders, $N stays verbatim, identifiers are re-emitted quoted,
// keywords lowercase, all joined by single spaces (PRD §8.4, ADR-0016).
// Whitespace/case normalization is deliberate — redacted SQL is display/AI
// material, never executed; payload_digest covers the original bytes.
//
// The input is this dialect's parse handle (the same shape Classify takes):
// requiring it keeps the fail-closed gate — the lexer only ever sees text
// that already parsed as exactly one statement — without re-parsing the SQL
// a second time in the BindNamed → ParseSingle → Classify → Redact pipeline.
// A Statement minted by anything else fails closed.
func (d *Dialect) Redact(st query.Statement) (query.Redaction, error) {
	codes, err := lexCodes()
	if err != nil {
		return query.Redaction{}, err
	}
	ps, ok := st.(*statement)
	if !ok || ps.node == nil {
		return query.Redaction{}, &query.Rejection{Reason: query.RejectNotAllowlisted}
	}

	var (
		parts    []string
		literals []query.LiteralType
	)
	lexer := parser.NewLexer(ps.text)
	for {
		tok := lexer.NextToken()
		if tok.Type == 0 { // EOF
			break
		}
		switch {
		case codes.literal[tok.Type] != "":
			lit := codes.literal[tok.Type]
			parts = append(parts, "<"+string(lit)+">")
			literals = append(literals, lit)
		case codes.ident[tok.Type]:
			parts = append(parts, `"`+strings.ReplaceAll(tok.Str, `"`, `""`)+`"`)
		case tok.Type == codes.param:
			parts = append(parts, "$"+strconv.FormatInt(tok.Ival, 10))
		case tok.Str != "":
			// Keywords carry their lowercase name; operators, punctuation,
			// and multi-char specials carry their exact text.
			parts = append(parts, tok.Str)
		default:
			return query.Redaction{}, errRedactUnknownToken
		}
	}
	if lexer.Err != nil {
		return query.Redaction{}, errRedactLexFailure
	}
	return query.Redaction{SQL: strings.Join(parts, " "), Literals: literals}, nil
}

var (
	errRedactUnknownToken = errors.New("pgdialect: redaction met a token it cannot re-emit")
	errRedactLexFailure   = errors.New("pgdialect: redaction lexing failed")
	errRedactProbeFailure = errors.New("pgdialect: lexer token probe failed")
)

// lexTokenCodes classifies the lexer's numeric token types. The values are
// unexported in pgparser, so they are learned once by lexing canonical
// single-token snippets — behavior-derived, no magic numbers, and a parser
// bump that changes them fails closed here instead of mis-redacting.
type lexTokenCodes struct {
	literal map[int]query.LiteralType
	ident   map[int]bool
	param   int
}

var lexCodes = sync.OnceValues(func() (*lexTokenCodes, error) {
	probe := func(src string) (int, error) {
		lexer := parser.NewLexer(src)
		tok := lexer.NextToken()
		if lexer.Err != nil || tok.Type == 0 {
			return 0, errRedactProbeFailure
		}
		return tok.Type, nil
	}

	codes := &lexTokenCodes{
		literal: make(map[int]query.LiteralType),
		ident:   make(map[int]bool),
	}
	for _, p := range []struct {
		src string
		lit query.LiteralType
	}{
		{"1", query.LiteralInteger},
		{"1.5", query.LiteralDecimal},
		{"'x'", query.LiteralString},
		{"u&'x'", query.LiteralString},
		{"b'0'", query.LiteralOther},
		{"x'1f'", query.LiteralOther},
	} {
		code, err := probe(p.src)
		if err != nil {
			return nil, err
		}
		codes.literal[code] = p.lit
	}
	for _, src := range []string{"abc", `u&"x"`} {
		code, err := probe(src)
		if err != nil {
			return nil, err
		}
		if codes.literal[code] != "" {
			return nil, errRedactProbeFailure
		}
		codes.ident[code] = true
	}
	var err error
	if codes.param, err = probe("$1"); err != nil {
		return nil, err
	}
	if codes.literal[codes.param] != "" || codes.ident[codes.param] {
		return nil, errRedactProbeFailure
	}
	return codes, nil
})
