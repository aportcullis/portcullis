package query

import "time"

// ExecutionDeadlineGrace extends the local execution deadline past the statement timeout so the target's own timeout fires and rolls back first, leaving time for connection setup and COMMIT (ADR-0021).
const ExecutionDeadlineGrace = 10 * time.Second

// Execution is one bound statement ready to run: the SQL with $N placeholders, the ordered argument values, and the class the statement was classified as. The class selects the transaction mode (read → read-only transaction; write/ddl → transaction with commit/rollback, PRD §8.2).
type Execution struct {
	SQL            string
	Args           []TypedValue
	Class          StatementClass
	MaxRows        int
	MaxResultBytes int64
	TimeoutSeconds int
	// Ungoverned skips the single-statement, catalog and limit safeguards; only adapter tests of raw transaction behavior set it, so the zero value stays governed.
	Ungoverned bool
}
