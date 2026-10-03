package pgdialect

import (
	"context"
	"encoding/json"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pgplex/pgparser/nodes"
)

// validateCatalog rejects callable candidates outside the trusted built-in catalog.
func validateCatalog(ctx context.Context, conn *pgconn.PgConn, parsed query.Statement) error {
	st, ok := parsed.(*statement)
	if !ok {
		return &query.Rejection{Reason: query.RejectNotAllowlisted}
	}
	functions := []string{}
	operators := []string{}
	types := []string{}
	var collect func(nodes.Node) error
	collect = func(node nodes.Node) error {
		switch value := node.(type) {
		case *nodes.FuncCall:
			if name, ok := singleCatalogName(value.Funcname); ok {
				functions = append(functions, name)
			}
		case *nodes.A_Expr:
			if name, ok := singleCatalogName(value.Name); ok {
				operators = append(operators, name)
			}
		case *nodes.SortBy:
			if name, ok := singleCatalogName(value.UseOp); ok {
				operators = append(operators, name)
			}
		case *nodes.TypeName:
			if value.Names != nil && len(value.Names.Items) > 0 {
				last, ok := value.Names.Items[len(value.Names.Items)-1].(*nodes.String)
				if !ok {
					return &query.Rejection{Reason: query.RejectNotAllowlisted}
				}
				if len(value.Names.Items) > 1 {
					schema, ok := value.Names.Items[0].(*nodes.String)
					if !ok || schema.Str != "pg_catalog" {
						return &query.Rejection{Reason: query.RejectNotAllowlisted}
					}
				}
				types = append(types, last.Str)
			}
		}
		children, err := childNodes(node)
		if err != nil {
			return err
		}
		for _, child := range children {
			if err := collect(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := collect(st.node); err != nil {
		return err
	}
	args := make([][]byte, 3)
	for idx, names := range [][]string{functions, operators, types} {
		encoded, err := json.Marshal(names)
		if err != nil {
			return err
		}
		args[idx] = encoded
	}
	// Reject a name when even one visible candidate is untrusted: overload resolution may prefer it despite pg_catalog being first.
	result := conn.ExecParams(ctx, `select
 not exists (
  select 1 from pg_catalog.pg_proc p
  where p.proname in (select jsonb_array_elements_text($1::jsonb))
    and pg_catalog.pg_function_is_visible(p.oid)
    and (p.oid >= 16384 or p.pronamespace <> 'pg_catalog'::regnamespace)
 ) and not exists (
  select 1 from pg_catalog.pg_operator o join pg_catalog.pg_proc p on p.oid=o.oprcode
  where o.oprname in (select jsonb_array_elements_text($2::jsonb))
    and pg_catalog.pg_operator_is_visible(o.oid)
    and (o.oid >= 16384 or o.oprnamespace <> 'pg_catalog'::regnamespace or p.oid >= 16384 or p.pronamespace <> 'pg_catalog'::regnamespace)
 ) and not exists (
  select 1 from pg_catalog.pg_type t
  where t.typname in (select jsonb_array_elements_text($3::jsonb))
    and pg_catalog.pg_type_is_visible(t.oid)
    and (t.oid >= 16384 or t.typnamespace <> 'pg_catalog'::regnamespace)
 )`, args, nil, nil, nil).Read()
	if result.Err != nil {
		return redactExecError(ctx, result.Err)
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 || string(result.Rows[0][0]) != "t" {
		return &query.Rejection{Reason: query.RejectNotAllowlisted}
	}
	return nil
}

func singleCatalogName(list *nodes.List) (string, bool) {
	if list == nil || len(list.Items) != 1 {
		return "", false
	}
	name, ok := list.Items[0].(*nodes.String)
	if !ok {
		return "", false
	}
	return name.Str, true
}
