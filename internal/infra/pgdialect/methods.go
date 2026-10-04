package pgdialect

import (
	"github.com/pgplex/pgparser/nodes"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

// defaultIndexAccessMethod is the method PostgreSQL uses when an index or exclusion constraint omits USING.
const defaultIndexAccessMethod = "btree"

// defaultTableAccessMethod is the only table access method a statement may name.
const defaultTableAccessMethod = "heap"

// builtinIndexAccessMethods are the index methods PostgreSQL ships; CREATE ACCESS METHOD can bind any other name to arbitrary handler code, so an unlisted method fails closed (ADR-0002).
var builtinIndexAccessMethods = map[string]struct{}{
	"btree":  {},
	"hash":   {},
	"gist":   {},
	"spgist": {},
	"gin":    {},
	"brin":   {},
}

// errUnlistedMethod refuses an access method, operator class or exclusion operator outside the allow-list.
var errUnlistedMethod = &query.Rejection{Reason: query.RejectNotAllowlisted}

// indexAccessMethodName resolves an omitted index method to the PostgreSQL default.
func indexAccessMethodName(written string) string {
	if written == "" {
		return defaultIndexAccessMethod
	}
	return written
}

// checkIndexAccessMethod admits an omitted or built-in index access method.
func checkIndexAccessMethod(written string) error {
	if _, ok := builtinIndexAccessMethods[indexAccessMethodName(written)]; !ok {
		return errUnlistedMethod
	}
	return nil
}

// checkTableAccessMethod admits an omitted table access method or heap.
func checkTableAccessMethod(written string) error {
	if written != "" && written != defaultTableAccessMethod {
		return errUnlistedMethod
	}
	return nil
}

// checkIndexStmt gates CREATE INDEX's access method and refuses pre-resolved exclusion operators, which only analyzed trees carry.
func checkIndexStmt(node nodes.Node) error {
	stmt, ok := node.(*nodes.IndexStmt)
	if !ok || stmt.ExcludeOpNames != nil {
		return errUnlistedMethod
	}
	return checkIndexAccessMethod(stmt.AccessMethod)
}

// checkIndexElem refuses an explicit operator class or its parameters: the class decides which support functions the index runs, and the default class is the only one the statement does not name.
func checkIndexElem(node nodes.Node) error {
	elem, ok := node.(*nodes.IndexElem)
	if !ok || elem.Opclass != nil || elem.Opclassopts != nil {
		return errUnlistedMethod
	}
	return nil
}

// checkPartitionElem refuses an explicit partition-key operator class for the same reason as checkIndexElem.
func checkPartitionElem(node nodes.Node) error {
	elem, ok := node.(*nodes.PartitionElem)
	if !ok || elem.Opclass != nil {
		return errUnlistedMethod
	}
	return nil
}

// checkConstraint gates an exclusion constraint's index access method and every WITH operator through the operator allow-list.
func checkConstraint(node nodes.Node) error {
	constraint, ok := node.(*nodes.Constraint)
	if !ok {
		return errUnlistedMethod
	}
	if err := checkIndexAccessMethod(constraint.AccessMethod); err != nil {
		return err
	}
	operators, err := exclusionOperators(constraint)
	if err != nil {
		return err
	}
	for _, operator := range operators {
		if err := checkOperatorName(operator); err != nil {
			return err
		}
	}
	return nil
}

// exclusionOperators returns each `element WITH operator` operator name; a malformed pair is a rejection, never a skip.
func exclusionOperators(constraint *nodes.Constraint) ([]*nodes.List, error) {
	if constraint.Exclusions == nil {
		return nil, nil
	}
	operators := make([]*nodes.List, 0, len(constraint.Exclusions.Items))
	for _, item := range constraint.Exclusions.Items {
		pair, ok := item.(*nodes.List)
		if !ok || len(pair.Items) != 2 {
			return nil, errUnlistedMethod
		}
		operator, ok := pair.Items[1].(*nodes.List)
		if !ok {
			return nil, errUnlistedMethod
		}
		operators = append(operators, operator)
	}
	return operators, nil
}

// checkCreateStmt gates CREATE TABLE's table access method.
func checkCreateStmt(node nodes.Node) error {
	stmt, ok := node.(*nodes.CreateStmt)
	if !ok {
		return errUnlistedMethod
	}
	return checkTableAccessMethod(stmt.AccessMethod)
}

// checkIntoClause gates the table access method of CREATE TABLE AS and SELECT INTO.
func checkIntoClause(node nodes.Node) error {
	into, ok := node.(*nodes.IntoClause)
	if !ok {
		return errUnlistedMethod
	}
	return checkTableAccessMethod(into.AccessMethod)
}
