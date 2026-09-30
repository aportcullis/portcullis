package pgdialect

import (
	"strings"

	"github.com/pgplex/pgparser/nodes"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

// Classify applies the ADR-0002 statement allow-list, then checks functions and operators in every admitted class, including DDL. Unknown read/write nodes fail closed.
func (d *Dialect) Classify(st query.Statement) (query.StatementClass, error) {
	ps, ok := st.(*statement)
	if !ok || ps.node == nil {
		return "", &query.Rejection{Reason: query.RejectNotAllowlisted}
	}
	class, err := classifyTop(ps.node)
	if err != nil {
		return "", err
	}
	if err := sweepEffects(ps.node); err != nil {
		return "", err
	}
	return class, nil
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
		// CONCURRENTLY cannot run inside a transaction block, and every execution is wrapped in one (PRD §8.2, fixture #30): an admitted statement that can never succeed must not reach the approval flow.
		if stmt.Concurrent {
			return "", &query.Rejection{Reason: query.RejectNonTransactional}
		}
		return query.ClassDDL, nil
	case *nodes.DropStmt:
		if stmt.Concurrent { // DROP INDEX CONCURRENTLY (fixture #31)
			return "", &query.Rejection{Reason: query.RejectNonTransactional}
		}
		return query.ClassDDL, nil
	case *nodes.CreateTableAsStmt:
		// Reject prepared statements whose SQL is unavailable to the classifier.
		if _, opaque := stmt.Query.(*nodes.ExecuteStmt); opaque {
			return "", &query.Rejection{Reason: query.RejectOpaqueCall}
		}
		return query.ClassDDL, nil
	case *nodes.CreateSchemaStmt:
		// Apply statement gates inside CREATE SCHEMA to prevent nested bypasses.
		if stmt.SchemaElts != nil {
			for _, element := range stmt.SchemaElts.Items {
				if _, err := classifyTop(element); err != nil {
					return "", err
				}
			}
		}
		return query.ClassDDL, nil
	case *nodes.CreateStmt, *nodes.TruncateStmt,
		*nodes.RenameStmt, *nodes.CommentStmt, *nodes.AlterTableStmt,
		*nodes.ViewStmt, *nodes.CreateSeqStmt:
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

// classifyReadTree walks a SELECT/VALUES tree: hidden DML (data-modifying CTEs) escalates to write, an IntoClause escalates to ddl (fixture #10), and row locking is rejected while the statement stays read — FOR UPDATE cannot run in a read-only transaction (fixture #28).
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

// classifyWriteTree walks a DML tree for unknown nodes; locking inside a write is ordinary row locking, and an IntoClause (not grammatical here) would only escalate further.
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

// classifyExplain admits only read-classified statements and rejects every ANALYZE option, regardless of its argument (ADR-0002).
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
