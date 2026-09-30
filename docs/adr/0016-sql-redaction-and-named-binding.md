# ADR-0016: SQL redaction & named-parameter binding

- **Status:** Accepted
- **Date:** 2026-07-19

## Context
PRD §8.4 fixes the redactor contract — audit and AI-review paths record SQL only after ① comment removal ② inline literal → typed placeholder ③ bind placeholders preserved, and any parse/redaction failure is **fail-closed**: the original text is never recorded, only `payload_digest` + statement class.
PRD §5.3 assigns "error redaction" to the dialect adapter, while §8.4 says one redactor is shared by every path — this ADR resolves that boundary, pins the placeholder format the PRD leaves open, and fixes the named-parameter syntax that §4.2 implies (`:start_date`) but never formalizes.

Verified against `github.com/pgplex/pgparser` v0.2.0 (the ADR-0001 pick; re-checked 2026-07-19): the package has **no deparser and no AST walk helper**, its exported lexer (`parser.NewLexer`, `Token{Type, Str, Ival, Loc}`) reports only token *start* offsets, and the lexer **silently skips comments** (`--` and nested `/* */`, per PG's `scan.l`).
Those three facts rule out AST-mutation redaction and drive the token-rebuild design below.

## Decision

### One contract, per-dialect implementations
The redaction *contract* — the three rules, the placeholder format, fail-closed semantics, and the output vocabulary (`query.Redaction`) — is cross-dialect and shared by the audit and AI-review paths.
The *implementation* is per-dialect (it needs the dialect's lexer); the PostgreSQL implementation lives in `internal/infra/pgdialect`.
Consumers depend on the contract via consumer-defined ports (code.md), never on a dialect package.

### Redaction = token-stream rebuild
The redacted SQL is **rebuilt from the token stream**, not spliced from the original text:

- Tokens are emitted left→right, joined by single spaces.
- Literal tokens become typed placeholders: `SCONST` → `<string>`, `ICONST` → `<integer>`, `FCONST` → `<decimal>`, `BCONST`/`XCONST` → `<other>`.
- Bind placeholders (`PARAM`, `$N`) are preserved verbatim (rule ③).
- Comments never reach the token stream (lexer skips them) — rule ① holds by construction.
- Identifiers are re-emitted **always quoted** (`"users"`): unquoted identifiers arrive already case-folded in `Token.Str`, so quoting is semantically exact and removes any ambiguity with keywords.
  Keywords are emitted lowercase.
- `TRUE` / `FALSE` / `NULL` are keywords, not literal tokens, and carry zero entropy — they are preserved verbatim.
  (Distinguishing grammar positions like `IS TRUE` from data would require the AST; rejected as needless complexity for zero-entropy tokens.)

Consequences, pinned deliberately: **whitespace and case are normalized** in redacted output.
Acceptable because redacted SQL is display/AI material and is never executed; `payload_digest` (PRD §4.4) covers the original bytes.
Placeholder atoms contain no inner spaces while every other token pair is space-separated, so a genuine `a<string>b` comparison rebuilds as `a < "string" > b` — placeholders are unambiguous by construction.

### Fail-closed
Any lexer/parse failure makes `Redact` return an error; callers record only `payload_digest` and the statement class.
Partial output is never returned.
The test suite asserts the property: *no literal byte sequence from the input appears in redacted output*.

### Redaction input is the bound statement
`Redact` receives the single statement **after** named-parameter binding (`$N` present) — that is what rule ③ "bind placeholders preserved" refers to — as the parse handle `ParseSingle` returned.
The output includes `Literals []LiteralType` (per-type counts of inline literals found) so the submit path can recommend parameter use (§8.4 last sentence).

### Named parameters: `:name`
- Syntax `:name`, matching PRD §4.2's examples; name charset `[A-Za-z_][A-Za-z0-9_]*`, max 64 chars.
- Detection is lexer-based: a `:` token whose next token is an `IDENT` starting exactly at `Loc+1` (no whitespace), **and** whose case-folded name is in the provided parameter set.
  PG array slices are excluded by the PREVIOUS token's class (amended 2026-07-20, twice): a glued colon whose previous token is `[`, `)`, `]`, an identifier, or a literal is subscript syntax — `arr[i:j]`, `arr[:hi]`, `arr[(i):j]`, `arr[fn(i):j]` — never a bind reference; after anything else (operator, comma, `(`, keyword) it is one.
  The escape hatch for binding inside a subscript is parentheses: `arr[(:x)]`.
  `::` lexes as `TYPECAST` and `:=` as `COLON_EQUALS`, so neither collides.
- Distinct names map to `$1..$N` in order of first appearance; repeated names reuse their number.
- Errors, all fail-closed: a `:name` with no provided value → error; a provided-but-unused parameter → error; a raw `$N` in user SQL → rejected (one parameter mechanism only — `$N` is adapter output, never input).
- The bound SQL is a **byte-exact splice** of the original (only `:name` spans replaced).
  Comments survive into the bound SQL; they are removed only in the *redacted* output.

### PostgreSQL parameter types (2026-09-30)
The declared type is part of the approved payload and also determines the parameter OID sent through `pgconn.ExecParams`: string → text, integer → int8, decimal → numeric, boolean → bool, date → date, timestamp → timestamptz, and UUID → uuid.
Values remain separate text-format bind arguments; the adapter does not rewrite SQL to add casts.
RFC 3339 timestamps preserve their instant.
Null carries no base type, so its OID is zero and PostgreSQL infers it from SQL context; an ambiguous null expression requires an explicit SQL cast.

Passing nil OIDs for all parameters would discard declared types and let standalone parameters default to text.
Tests therefore use standalone typed parameters without casts as well as the existing explicit-cast scenarios.
Source (checked 2026-09-30): https://pkg.go.dev/github.com/jackc/pgx/v5/pgconn#PgConn.ExecParams

### Pipeline order (normative)
`BindNamed → ParseSingle → Classify → Redact(stmt)` — `:name` is not valid PG SQL, so binding must precede parsing.
`BindNamed` runs even with zero parameters (it validates that the SQL contains no stray `:name`/`$N`).
`Redact` takes the parse handle `ParseSingle` produced (amended 2026-07-20) — the fail-closed "only parsed input is lexed" gate holds by construction, without re-parsing the same SQL a second time in the pipeline.

### Error-text hygiene
- Parser/lexer error messages may quote the input (and thus literals) — they are never propagated.
  Only a byte offset crosses the boundary (`query.ParseFailure{Position}`).
- Target-DB execution errors surface **to the requester only** as `SQLState + Message + Position`; `Detail`/`Hint`/`Where`/`InternalQuery` are dropped (they can embed row data, e.g. unique-violation keys).
  Audit/log records get SQLState + class only.
  The primary Message can itself quote input values (e.g. 22P02 embeds the rejected literal), so it travels as a requester-facing FIELD and is excluded from `ExecError.Error()` — the string a log line would capture (amended 2026-07-20).
  Connection-phase failures keep the existing 6-bucket redaction (ADR-0014).

### `payload_digest` — not pinned here
PRD §4.4 already pins it (keyed HMAC-SHA-256 over the canonical payload, HKDF payload-integrity key, ADR-0003).
It is shared-service work landing with the access_requests slice; this ADR only cross-references it.
The digest is computed over the exact SQL **before** encryption/redaction.

## Consequences
- The MySQL/SQLite adapters (M2) must implement the same contract: same placeholder atoms, same fail-closed semantics, same `:name` syntax, driven by their own lexers; the shared contract tests carry the redaction fixtures the same way ADR-0002 carries classification.
- Redacted output is normalized (case/whitespace); anyone needing the original consults the AEAD-encrypted payload (authorized approvers only, PRD §8.4), never audit rows.
- `Column.Nullable` cannot be derived from the wire protocol in the execution path; the result-store slice decides whether a catalog lookup is worth adding (noted so the gap is deliberate, not accidental).
- If the exported lexer proves unable to reproduce keyword/operator text faithfully, the fallback is **not** a hand-rolled scanner (that would break fail-closed guarantees): the redactor fails, callers fall back to digest+class, and this ADR is amended.
