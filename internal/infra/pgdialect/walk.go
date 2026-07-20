package pgdialect

import (
	"reflect"

	"github.com/pgplex/pgparser/nodes"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

// treeFacts is what a full-tree walk learns about a statement.
type treeFacts struct {
	dml     bool // InsertStmt/UpdateStmt/DeleteStmt/MergeStmt anywhere
	into    bool // IntoClause anywhere (SELECT … INTO creates an object)
	locking bool // LockingClause anywhere (FOR UPDATE/SHARE …)
}

// allowedTags is the expression/clause vocabulary a read or write tree may
// contain (ADR-0002 allow-list). Anything outside it — plus the special
// cases handled in visit — rejects the statement. The set grows only
// deliberately, with a fixture or a pinned regression sample.
var allowedTags = func() map[nodes.NodeTag]struct{} {
	tags := []nodes.NodeTag{
		nodes.T_SelectStmt, // subqueries, CTE bodies, VALUES
		nodes.T_List,
		nodes.T_A_Expr,
		nodes.T_A_Const,
		nodes.T_A_Star,
		nodes.T_A_Indirection,
		nodes.T_A_Indices,
		nodes.T_A_ArrayExpr,
		nodes.T_BoolExpr,
		nodes.T_NullTest,
		nodes.T_BooleanTest,
		nodes.T_SubLink,
		nodes.T_CaseExpr,
		nodes.T_CaseWhen,
		nodes.T_CoalesceExpr,
		nodes.T_MinMaxExpr,
		nodes.T_SQLValueFunction,
		nodes.T_GroupingFunc,
		nodes.T_TypeCast,
		nodes.T_TypeName,
		nodes.T_CollateClause,
		nodes.T_ColumnRef,
		nodes.T_ParamRef,
		nodes.T_FuncCall,
		nodes.T_WindowDef,
		nodes.T_SortBy,
		nodes.T_RangeVar,
		nodes.T_RangeSubselect,
		nodes.T_RangeFunction,
		nodes.T_Alias,
		nodes.T_JoinExpr,
		nodes.T_ResTarget,
		nodes.T_WithClause,
		nodes.T_CommonTableExpr,
		nodes.T_GroupingSet,
		nodes.T_RowExpr,
		nodes.T_NamedArgExpr,
		// DML-internal clauses (write trees).
		nodes.T_OnConflictClause,
		nodes.T_InferClause,
		nodes.T_MergeWhenClause,
		nodes.T_SetToDefault,
		nodes.T_MultiAssignRef,
		// Value wrappers.
		nodes.T_String,
		nodes.T_Integer,
		nodes.T_Float,
		nodes.T_Boolean,
		nodes.T_BitString,
	}
	set := make(map[nodes.NodeTag]struct{}, len(tags))
	for _, t := range tags {
		set[t] = struct{}{}
	}
	return set
}()

// walk visits every node reachable from root and returns the collected
// facts, rejecting the statement on the first node outside the allow-list —
// ADR-0002: an unknown node anywhere in the tree fails closed.
func walk(root nodes.Node) (treeFacts, error) {
	var facts treeFacts
	if err := visit(root, &facts); err != nil {
		return treeFacts{}, err
	}
	return facts, nil
}

func visit(node nodes.Node, facts *treeFacts) error {
	switch node.Tag() {
	case nodes.T_InsertStmt, nodes.T_UpdateStmt, nodes.T_DeleteStmt, nodes.T_MergeStmt:
		facts.dml = true
	case nodes.T_IntoClause:
		facts.into = true
	case nodes.T_LockingClause:
		facts.locking = true
	default:
		if _, ok := allowedTags[node.Tag()]; !ok {
			return &query.Rejection{Reason: query.RejectNotAllowlisted}
		}
	}
	kids, err := childNodes(node)
	if err != nil {
		return err
	}
	for _, child := range kids {
		if err := visit(child, facts); err != nil {
			return err
		}
	}
	return nil
}

// errUntraversable marks a node whose reflected shape the walker cannot fully
// inspect (a node-bearing field reflection refuses to read — an unexported
// one). The classifier rejects rather than risk missing a node: an allow-list
// that silently skips part of the tree fails OPEN, contrary to ADR-0002's
// "unknown node anywhere fails closed". pgparser v0.2.0's parse nodes have no
// such field today — this keeps the guarantee if a future bump adds one, and
// replaces the reflect.Value.Interface() panic that would otherwise occur.
var errUntraversable = &query.Rejection{Reason: query.RejectNotAllowlisted}

// childNodes returns the nodes.Node values one level below node. pgparser
// exposes no walker, so reflection is the traversal; the node structs are
// plain data mirroring parsenodes.h.
func childNodes(node nodes.Node) ([]nodes.Node, error) {
	v := reflect.ValueOf(node)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, nil
	}
	var out []nodes.Node
	for i := range v.NumField() {
		if err := collectNodes(v.Field(i), &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// collectNodes appends every nodes.Node reachable from v to out, descending
// through pointers, interfaces, slices, arrays, maps, and by-value structs so
// no reference shape is silently skipped (a missed node would defeat the
// allow-list). A node-bearing field reflection refuses to read is fail-closed
// via errUntraversable; scalar leaves carry no nodes and are ignored.
func collectNodes(v reflect.Value, out *[]nodes.Node) error {
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		if !v.CanInterface() {
			return errUntraversable
		}
		if n, ok := v.Interface().(nodes.Node); ok {
			*out = append(*out, n) // visit() recurses into it
			return nil
		}
		return collectNodes(v.Elem(), out) // non-Node wrapper: descend into it
	case reflect.Struct:
		for i := range v.NumField() {
			if err := collectNodes(v.Field(i), out); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			if err := collectNodes(v.Index(i), out); err != nil {
				return err
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			if err := collectNodes(iter.Value(), out); err != nil {
				return err
			}
		}
	}
	return nil
}
