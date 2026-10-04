package pgdialect

import (
	"github.com/pgplex/pgparser/nodes"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

// Reject unlisted and qualified functions in every statement class (ADR-0002). READ ONLY and volatility do not prevent all side effects; target privileges and pinned OID resolution remain required. Extend the allow-list with a classification fixture.
var allowedFunctions = func() map[string]struct{} {
	names := []string{
		// Aggregates and their common companions.
		"count", "sum", "avg", "min", "max", "array_agg", "string_agg",
		"jsonb_agg", "json_agg", "jsonb_object_agg", "json_object_agg",
		"bool_and", "bool_or", "every", "stddev", "stddev_pop", "stddev_samp",
		"variance", "var_pop", "var_samp",
		// Window functions.
		"row_number", "rank", "dense_rank", "percent_rank", "cume_dist",
		"ntile", "lag", "lead", "first_value", "last_value", "nth_value",
		// Strings.
		"lower", "upper", "initcap", "length", "char_length", "character_length",
		"substr", "substring", "left", "right", "trim", "btrim", "ltrim", "rtrim",
		"lpad", "rpad", "replace", "split_part", "concat", "concat_ws", "format",
		"position", "strpos", "starts_with", "reverse", "repeat", "md5",
		"to_hex", "quote_ident", "quote_literal", "quote_nullable",
		"encode", "decode", "translate", "overlay", "ascii", "chr",
		// Pattern matching (regexp_* are immutable; regexp_replace included).
		"regexp_replace", "regexp_match", "regexp_matches", "regexp_split_to_array",
		"regexp_split_to_table", "like_escape", "similar_to_escape",
		// Numbers.
		"abs", "ceil", "ceiling", "floor", "round", "trunc", "sign", "mod",
		"power", "sqrt", "cbrt", "exp", "ln", "log", "log10", "div",
		"greatest", "least", "width_bucket", "gcd", "lcm",
		// Dates and times (now()/current_* are STABLE, not volatile).
		"now", "age", "date_part", "date_trunc", "date_bin", "extract",
		"make_date", "make_time", "make_timestamp", "make_timestamptz",
		"make_interval", "to_char", "to_date", "to_number", "to_timestamp",
		"justify_days", "justify_hours", "justify_interval", "isfinite",
		"timezone", "date_add", "date_subtract",
		// JSON/JSONB (read-side helpers only).
		"to_json", "to_jsonb", "jsonb_build_object", "json_build_object",
		"jsonb_build_array", "json_build_array", "jsonb_array_elements",
		"json_array_elements", "jsonb_array_elements_text",
		"json_array_elements_text", "jsonb_array_length", "json_array_length",
		"jsonb_each", "json_each", "jsonb_each_text", "json_each_text",
		"jsonb_object_keys", "json_object_keys", "jsonb_extract_path",
		"json_extract_path", "jsonb_extract_path_text", "json_extract_path_text",
		"jsonb_typeof", "json_typeof", "jsonb_strip_nulls", "json_strip_nulls",
		"jsonb_pretty", "jsonb_path_query", "jsonb_path_exists",
		// Arrays.
		"array_length", "array_lower", "array_upper", "array_ndims",
		"array_dims", "array_position", "array_positions", "array_to_string",
		"string_to_array", "cardinality", "unnest", "array_append",
		"array_prepend", "array_cat", "array_remove", "array_replace",
		// Rows/sets a read query legitimately uses.
		"generate_series", "generate_subscripts", "coalesce", "nullif",
		"num_nulls", "num_nonnulls",
		// Type/format helpers.
		"cast", "to_ascii", "convert_from", "convert_to", "bit_length",
		"octet_length", "pg_typeof",
	}
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return set
}()

// catalogSchema is the only schema a grammar-rewritten call may carry.
const catalogSchema = "pg_catalog"

// grammarCatalogFunctions maps each pg_catalog function the PostgreSQL grammar substitutes for SQL-standard syntax to the call form the grammar records: EXTRACT, SUBSTRING … FROM, POSITION … IN, OVERLAY … PLACING, TRIM and AT TIME ZONE/AT LOCAL are marked as SQL syntax, while LIKE/SIMILAR … ESCAPE build an ordinary call. A user-written `pg_catalog.extract(…)` is an explicit call and stays refused, like every other qualified name (ADR-0002).
var grammarCatalogFunctions = map[string]nodes.CoercionForm{
	"extract":           nodes.COERCE_SQL_SYNTAX,
	"substring":         nodes.COERCE_SQL_SYNTAX,
	"position":          nodes.COERCE_SQL_SYNTAX,
	"overlay":           nodes.COERCE_SQL_SYNTAX,
	"btrim":             nodes.COERCE_SQL_SYNTAX,
	"ltrim":             nodes.COERCE_SQL_SYNTAX,
	"rtrim":             nodes.COERCE_SQL_SYNTAX,
	"timezone":          nodes.COERCE_SQL_SYNTAX,
	"like_escape":       nodes.COERCE_EXPLICIT_CALL,
	"similar_to_escape": nodes.COERCE_EXPLICIT_CALL,
}

// checkFuncCall rejects a function call the allow-list does not cover. A qualified name is rejected — the walker cannot know which object it resolves to — except the exact two-part `pg_catalog.<name>` form the grammar itself produces for SQL-standard syntax. An unreadable/empty name is likewise a rejection, never a pass.
func checkFuncCall(node nodes.Node) error {
	call, ok := node.(*nodes.FuncCall)
	if !ok {
		return &query.Rejection{Reason: query.RejectNotAllowlisted}
	}
	if name, ok := singleCatalogName(call.Funcname); ok {
		if _, allowed := allowedFunctions[name]; allowed {
			return nil
		}
		return &query.Rejection{Reason: query.RejectNotAllowlisted}
	}
	if _, ok := grammarCatalogFunctionName(call); ok {
		return nil
	}
	return &query.Rejection{Reason: query.RejectNotAllowlisted}
}

// grammarCatalogFunctionName returns the function name when call is exactly a grammar-produced `pg_catalog.<name>` call of an allow-listed function in the form the grammar records.
func grammarCatalogFunctionName(call *nodes.FuncCall) (string, bool) {
	if call.Funcname == nil || len(call.Funcname.Items) != 2 {
		return "", false
	}
	schema, ok := call.Funcname.Items[0].(*nodes.String)
	if !ok || schema.Str != catalogSchema {
		return "", false
	}
	name, ok := call.Funcname.Items[1].(*nodes.String)
	if !ok {
		return "", false
	}
	form, rewritten := grammarCatalogFunctions[name.Str]
	if !rewritten || nodes.CoercionForm(call.FuncFormat) != form {
		return "", false
	}
	if _, allowed := allowedFunctions[name.Str]; !allowed {
		return "", false
	}
	return name.Str, true
}

// allowedOperators is the operator vocabulary an approved statement may use. An operator IS a function call in disguise — `CREATE OPERATOR` binds an arbitrary function to a symbol — so `a ### b` deserves exactly the scrutiny `my_udf(a, b)` gets. Only plainly pure standard operators are listed.
var allowedOperators = func() map[string]struct{} {
	symbols := []string{
		// Comparison.
		"=", "<>", "!=", "<", ">", "<=", ">=",
		// Arithmetic (# and & etc. are excluded: bit/geometry surface we do not need yet, and an unlisted operator is a support ticket, not a breach).
		"+", "-", "*", "/", "%", "^", "|/", "||/", "@",
		// Text.
		"||",
		// Pattern matching — the names the grammar gives LIKE/ILIKE/SIMILAR are checked here too when written as bare operators.
		"~~", "!~~", "~~*", "!~~*", "~", "!~", "~*", "!~*",
		// jsonb / array accessors and containment.
		"->", "->>", "#>", "#>>", "@>", "<@", "?", "?|", "?&", "&&", "@?", "@@",
	}
	set := make(map[string]struct{}, len(symbols))
	for _, s := range symbols {
		set[s] = struct{}{}
	}
	return set
}()

// checkOperator gates user-named operators and rejects unknown expression kinds; grammar-fixed names require no lookup.
func checkOperator(node nodes.Node) error {
	expr, ok := node.(*nodes.A_Expr)
	if !ok {
		return &query.Rejection{Reason: query.RejectNotAllowlisted}
	}
	switch expr.Kind {
	case nodes.AEXPR_OP, nodes.AEXPR_OP_ANY, nodes.AEXPR_OP_ALL:
		// The name below is whatever the statement wrote — gate it.
	case nodes.AEXPR_DISTINCT, nodes.AEXPR_NOT_DISTINCT, nodes.AEXPR_NULLIF,
		nodes.AEXPR_IN, nodes.AEXPR_LIKE, nodes.AEXPR_ILIKE, nodes.AEXPR_SIMILAR,
		nodes.AEXPR_BETWEEN, nodes.AEXPR_NOT_BETWEEN,
		nodes.AEXPR_BETWEEN_SYM, nodes.AEXPR_NOT_BETWEEN_SYM:
		return nil
	default:
		return &query.Rejection{Reason: query.RejectNotAllowlisted}
	}
	return checkOperatorName(expr.Name)
}

// checkSortBy gates ORDER BY USING operators stored outside A_Expr so arbitrary operator functions cannot bypass the effect check.
func checkSortBy(node nodes.Node) error {
	sort, ok := node.(*nodes.SortBy)
	if !ok {
		return &query.Rejection{Reason: query.RejectNotAllowlisted}
	}
	if sort.SortbyDir != nodes.SORTBY_USING && sort.UseOp == nil {
		return nil
	}
	return checkOperatorName(sort.UseOp)
}

// checkSubLink gates subquery-comparison operators stored outside A_Expr; IN, EXISTS, scalar and ARRAY subqueries carry no written operator.
func checkSubLink(node nodes.Node) error {
	link, ok := node.(*nodes.SubLink)
	if !ok {
		return &query.Rejection{Reason: query.RejectNotAllowlisted}
	}
	if link.OperName == nil {
		return nil
	}
	return checkOperatorName(link.OperName)
}

// checkOperatorName gates a user-written operator name wherever the grammar carries one (A_Expr.Name, SortBy.UseOp, SubLink.OperName). A qualified name — `OPERATOR(public.###)` — rejects for the same reason a qualified function name does: the walker cannot resolve what it binds to. A missing/unreadable name is likewise a rejection, never a pass.
func checkOperatorName(name *nodes.List) error {
	if name == nil || len(name.Items) != 1 {
		return &query.Rejection{Reason: query.RejectNotAllowlisted}
	}
	sym, ok := name.Items[0].(*nodes.String)
	if !ok {
		return &query.Rejection{Reason: query.RejectNotAllowlisted}
	}
	if _, allowed := allowedOperators[sym.Str]; !allowed {
		return &query.Rejection{Reason: query.RejectNotAllowlisted}
	}
	return nil
}
