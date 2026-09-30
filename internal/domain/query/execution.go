package query

// Execution is one bound statement ready to run: the SQL with $N placeholders, the ordered argument values, and the class the statement was classified as. The class selects the transaction mode (read → read-only transaction; write/ddl → transaction with commit/rollback, PRD §8.2).
type Execution struct {
	SQL   string
	Args  []TypedValue
	Class StatementClass
}
