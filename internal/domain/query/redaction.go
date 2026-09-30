package query

// LiteralType tags an inline literal found during redaction so the submit path can recommend parameters (PRD §8.4, ADR-0016).
type LiteralType string

const (
	LiteralString  LiteralType = "string"
	LiteralInteger LiteralType = "integer"
	LiteralDecimal LiteralType = "decimal"
	LiteralOther   LiteralType = "other"
)

// Redaction is the safe-to-record form of one bound statement: comments removed, inline literals replaced with typed placeholders, bind placeholders preserved (PRD §8.4). Literals lists, in order of appearance, the inline literals that were replaced.
type Redaction struct {
	SQL      string
	Literals []LiteralType
}
