package pgdialect

import (
	"github.com/pgplex/pgparser/nodes"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

// managedObjectKinds are the object kinds the CREATE allow-list can produce (table, view, materialized view, index, sequence, schema); RENAME, DROP and COMMENT may act only on these, so cluster-level objects, routines, triggers, policies and extensions fail closed (ADR-0002).
var managedObjectKinds = map[nodes.ObjectType]struct{}{
	nodes.OBJECT_TABLE:    {},
	nodes.OBJECT_VIEW:     {},
	nodes.OBJECT_MATVIEW:  {},
	nodes.OBJECT_INDEX:    {},
	nodes.OBJECT_SEQUENCE: {},
	nodes.OBJECT_SCHEMA:   {},
}

// columnOwnerKinds are the relation kinds whose columns RENAME and COMMENT may address.
var columnOwnerKinds = map[nodes.ObjectType]struct{}{
	nodes.OBJECT_TABLE:   {},
	nodes.OBJECT_VIEW:    {},
	nodes.OBJECT_MATVIEW: {},
}

// allowedAlterTableSubcommands is the explicit ALTER TABLE vocabulary: column add/drop/type change, default and NOT NULL changes, and table-constraint add/drop/validate. Ownership, trigger/rule toggles, row-security toggles, replica identity, access method, tablespace, inheritance and partition changes are refused (ADR-0002).
var allowedAlterTableSubcommands = map[nodes.AlterTableType]struct{}{
	nodes.AT_AddColumn:          {},
	nodes.AT_DropColumn:         {},
	nodes.AT_AlterColumnType:    {},
	nodes.AT_ColumnDefault:      {},
	nodes.AT_SetNotNull:         {},
	nodes.AT_DropNotNull:        {},
	nodes.AT_AddConstraint:      {},
	nodes.AT_DropConstraint:     {},
	nodes.AT_ValidateConstraint: {},
}

// errUnmanagedObject refuses DDL addressing an object kind or subcommand outside the allow-list.
var errUnmanagedObject = &query.Rejection{Reason: query.RejectNotAllowlisted}

// checkRenameTarget admits renaming a managed object, a column of a table/view/materialized view, or a table constraint.
func checkRenameTarget(stmt *nodes.RenameStmt) error {
	switch stmt.RenameType {
	case nodes.OBJECT_COLUMN:
		return requireKind(columnOwnerKinds, stmt.RelationType)
	case nodes.OBJECT_TABCONSTRAINT:
		if stmt.RelationType != nodes.OBJECT_TABLE {
			return errUnmanagedObject
		}
		return nil
	default:
		return requireKind(managedObjectKinds, stmt.RenameType)
	}
}

// checkDropTarget admits dropping only managed object kinds.
func checkDropTarget(stmt *nodes.DropStmt) error {
	return requireKind(managedObjectKinds, nodes.ObjectType(stmt.RemoveType))
}

// checkCommentTarget admits commenting on a managed object, a column, or a table constraint.
func checkCommentTarget(stmt *nodes.CommentStmt) error {
	switch stmt.Objtype {
	case nodes.OBJECT_COLUMN, nodes.OBJECT_TABCONSTRAINT:
		return nil
	default:
		return requireKind(managedObjectKinds, stmt.Objtype)
	}
}

// checkAlterTable admits ALTER TABLE on a plain table whose every subcommand is allow-listed; one refused subcommand refuses the statement.
func checkAlterTable(stmt *nodes.AlterTableStmt) error {
	if nodes.ObjectType(stmt.ObjType) != nodes.OBJECT_TABLE || stmt.Cmds == nil || len(stmt.Cmds.Items) == 0 {
		return errUnmanagedObject
	}
	for _, item := range stmt.Cmds.Items {
		cmd, ok := item.(*nodes.AlterTableCmd)
		if !ok {
			return errUnmanagedObject
		}
		if _, allowed := allowedAlterTableSubcommands[nodes.AlterTableType(cmd.Subtype)]; !allowed {
			return errUnmanagedObject
		}
	}
	return nil
}

// requireKind refuses an object kind outside allowed.
func requireKind(allowed map[nodes.ObjectType]struct{}, kind nodes.ObjectType) error {
	if _, ok := allowed[kind]; !ok {
		return errUnmanagedObject
	}
	return nil
}
