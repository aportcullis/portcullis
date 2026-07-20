package pgdialect

import (
	"strings"

	"github.com/pgplex/pgparser/nodes"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

// Classify determines the privilege class of a parsed statement per the
// ADR-0002 allow-list. Read and write trees are walked in full — an unknown
// node anywhere rejects, hidden DML escalates, SELECT … INTO escalates to
// ddl. Top-level DDL forms classify by form alone: ddl is already the
// highest class, so nothing inside can escalate it, and walking the DDL
// grammar would only false-reject forms the ADR-0002 table explicitly allows.
func (d *Dialect) Classify(st query.Statement) (query.StatementClass, error) {
	ps, ok := st.(*statement)
	if !ok || ps.node == nil {
		return "", &query.Rejection{Reason: query.RejectNotAllowlisted}
	}
	return classifyTop(ps.node)
}

func classifyTop(node nodes.Node) (query.StatementClass, error) {
	switch stmt := node.(type) {
	case *nodes.SelectStmt:
		return classifyReadTree(node)
	case *nodes.VariableShowStmt:
		// SHOW x — a read-only leaf (name only, no sub-tree).
		return query.ClassRead, nil
	case *nodes.ExplainStmt:
		return classifyExplain(stmt)
	case *nodes.InsertStmt, *nodes.UpdateStmt, *nodes.DeleteStmt, *nodes.MergeStmt:
		return classifyWriteTree(node)
	case *nodes.IndexStmt:
		// CONCURRENTLY cannot run inside a transaction block, and every
		// execution is wrapped in one (PRD §8.2, fixture #30): an admitted
		// statement that can never succeed must not reach the approval flow.
		if stmt.Concurrent {
			return "", &query.Rejection{Reason: query.RejectNonTransactional}
		}
		return query.ClassDDL, nil
	case *nodes.DropStmt:
		if stmt.Concurrent { // DROP INDEX CONCURRENTLY (fixture #31)
			return "", &query.Rejection{Reason: query.RejectNonTransactional}
		}
		return query.ClassDDL, nil
	case *nodes.CreateStmt, *nodes.CreateTableAsStmt, *nodes.TruncateStmt,
		*nodes.RenameStmt, *nodes.CommentStmt, *nodes.AlterTableStmt,
		*nodes.ViewStmt, *nodes.CreateSeqStmt, *nodes.CreateSchemaStmt:
		return query.ClassDDL, nil
	case *nodes.TransactionStmt:
		return "", &query.Rejection{Reason: query.RejectTxnControl}
	case *nodes.VariableSetStmt:
		return "", &query.Rejection{Reason: query.RejectSessionMutation}
	case *nodes.CopyStmt:
		return "", &query.Rejection{Reason: query.RejectFileAccess}
	case *nodes.CallStmt, *nodes.DoStmt:
		return "", &query.Rejection{Reason: query.RejectOpaqueCall}
	default:
		return "", &query.Rejection{Reason: query.RejectNotAllowlisted}
	}
}

// classifyReadTree walks a SELECT/VALUES tree: hidden DML (data-modifying
// CTEs) escalates to write, an IntoClause escalates to ddl (fixture #10),
// and row locking is rejected while the statement stays read — FOR UPDATE
// cannot run in a read-only transaction (fixture #28).
func classifyReadTree(node nodes.Node) (query.StatementClass, error) {
	facts, err := walk(node)
	if err != nil {
		return "", err
	}
	class := query.ClassRead
	if facts.dml {
		class = query.ClassWrite
	}
	if facts.into {
		class = query.ClassDDL
	}
	if facts.locking && class == query.ClassRead {
		return "", &query.Rejection{Reason: query.RejectLocking}
	}
	return class, nil
}

// classifyWriteTree walks a DML tree for unknown nodes; locking inside a
// write is ordinary row locking, and an IntoClause (not grammatical here)
// would only escalate further.
func classifyWriteTree(node nodes.Node) (query.StatementClass, error) {
	facts, err := walk(node)
	if err != nil {
		return "", err
	}
	if facts.into {
		return query.ClassDDL, nil
	}
	return query.ClassWrite, nil
}

// classifyExplain allows EXPLAIN of a read-classified statement only
// (ADR-0002): any ANALYZE option — bare or parenthesized, regardless of its
// argument — rejects. The inner statement goes through the SAME classifier as
// a top-level one (nested EXPLAIN cannot parse, so the recursion is one
// level), and anything that does not come out as read is rejected — the
// read-only policy is a class requirement, not a node-type list of its own.
func classifyExplain(stmt *nodes.ExplainStmt) (query.StatementClass, error) {
	if stmt.Options != nil {
		for _, opt := range stmt.Options.Items {
			def, ok := opt.(*nodes.DefElem)
			if !ok {
				return "", &query.Rejection{Reason: query.RejectNotAllowlisted}
			}
			if strings.EqualFold(def.Defname, "analyze") {
				return "", &query.Rejection{Reason: query.RejectExplainAnalyze}
			}
		}
	}
	if stmt.Query == nil {
		return "", &query.Rejection{Reason: query.RejectNotAllowlisted}
	}
	class, err := classifyTop(stmt.Query)
	if err != nil {
		return "", err
	}
	if class != query.ClassRead {
		return "", &query.Rejection{Reason: query.RejectNotAllowlisted}
	}
	return query.ClassRead, nil
}
