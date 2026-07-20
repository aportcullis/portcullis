package pgdialect

import (
	"errors"

	"github.com/pgplex/pgparser/nodes"
	"github.com/pgplex/pgparser/parser"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

// statement is this dialect's private parse handle. Classify type-asserts it
// back; a Statement built by anything else fails closed.
type statement struct {
	text string
	node nodes.Node
}

func (s *statement) Text() string { return s.text }

// ParseSingle parses sql with the real PostgreSQL grammar (pgparser,
// ADR-0001) and requires exactly one statement (ADR-0002). Parser error
// messages can quote the input, so failures carry only a byte offset.
func (d *Dialect) ParseSingle(sql string) (query.Statement, error) {
	list, err := parser.Parse(sql)
	if err != nil {
		var pe *parser.ParseError
		if errors.As(err, &pe) {
			return nil, &query.ParseFailure{Position: pe.Position}
		}
		return nil, &query.ParseFailure{}
	}
	// Empty and comment-only input both yield a nil/empty list (verified
	// against pgparser v0.2.0).
	if list.Len() == 0 {
		return nil, query.ErrEmptyStatement
	}
	if list.Len() > 1 {
		return nil, query.ErrMultipleStatements
	}
	return &statement{text: sql, node: list.Items[0]}, nil
}
