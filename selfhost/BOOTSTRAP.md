# Bootstrapping machin in machin

The goal: a machin compiler **written in MFL** that compiles its own source — the
"it compiles itself" milestone, and the ultimate dogfood (the compiler is the
hardest possible app, so it surfaces the language's real gaps).

## Method: oracle-diff every stage

The current compiler is ~8.7K LOC of Go across five stages:

| stage | Go file | LOC | MFL port |
|-------|---------|-----|----------|
| lex | `lexer.go` | 180 | `selfhost/lex.src` ✅ |
| parse | `parser.go` + `ast.go` | 1547 | `selfhost/parse.src` |
| typecheck/infer | `types.go` | 2697 | `selfhost/check.src` |
| C codegen | `codegen.go` | 4270 | `selfhost/cgen.src` |
| driver/normalize | `transform.go` + `main.go` | ~750 | `selfhost/main.src` |

Porting 8.7K LOC blind is intractable. The discipline that makes it tractable:
**each MFL stage is proven byte-for-byte equivalent to its Go reference before the
next stage starts.** For each stage the Go binary grows a hidden `*test` subcommand
that dumps that stage's output in a canonical, escaping-free form; the MFL stage
emits the identical format; we diff over the whole `.src` corpus (framework modules
+ every dogfood tool). A stage isn't "done" until the diff is empty across the corpus.

## Status

### Stage 1 — lexer ✅
`selfhost/lex.src` is a port of `lexer.go`. Oracle: `machin lextest <file>` (Go
lexer) prints one token per line as `<kind> <pos> <hex(val)>`; the MFL lexer emits
the same. **Verified identical across 70 corpus files / 77,337 tokens, 0
mismatches.** Run `selfhost/verify.sh`.

Notes from the port (MFL was already enough — no new builtin needed):
- byte-exact scanning via `read_file_bytes`+`byte_at`; token spans via
  `read_file`+`substr` (both byte-indexed, matching Go slicing).
- hex-encoding the value in the oracle format sidesteps every newline/quote/UTF-8
  escaping mismatch between Go and MFL.
- no receiver methods in MFL → the sub-lexers are inlined into one flat `lex()`.

### Stage 2 — parser (in progress)
Port `parser.go` → an AST. **AST representation decision:** MFL has no sum types, so
the AST is a **flat index-array of fat nodes** (`selfhost/parse.src`: `var nodes
[]Node`, children referenced by integer index). No recursive types, no deep struct
copies — cache-friendly, which matters for the perf gate. Nodes are built bottom-up
and appended whole (no post-append mutation), sidestepping any slice-element-field
mutation question.

Oracle: `machin parsetest --expr <e>` dumps the AST as canonical, fully-parenthesized
S-expressions (string values hex-encoded); the MFL parser emits the identical form.

**Done: the expression grammar** (`parseExpr`..`parsePrimary` + precedence climbing,
calls, spread, index, field chains, call-value, slice/struct literals, make,
unary/recv, all literals). **Verified across 38 expression forms, 0 mismatches**
(`selfhost/verify-parse.sh`). No new builtin needed.

Known gap (deferred): float literals are dumped from the lexeme normalized to Go's
shortest form (`2.0`→`2`); the very-large-magnitude case where Go's `%v` switches to
`1e+NN` isn't matched (absent from real source). Clean fix later: dump IEEE-754 bits
(would want a `float_bits` builtin — a real addition the bootstrap drives).

**Still to port for Stage 2:** statements (`parseStmt`/`parseBlock`/if/while/for/
range/select/multi-assign), declarations (`parseFuncDecl`/`parseTypeDecl`/
`parseExternDecl`/`ParseGlobal`), the two-pass program driver (types first to seed
known-struct names), and whole-`.mfl`-program AST parity over the corpus. FuncLit
bodies and `T{...}` struct literals light up once statements + the known-struct set
land (currently stubbed: `(funclit)`, `is_known_struct → false`).

### Stages 3–5 — typecheck, codegen, driver
Same oracle-diff discipline. Codegen verifies two ways: diff the emitted C against
Go codegen, **and** compile it + run the existing test corpus.

### Stage 6 — fixpoint (self-hosting achieved)
`mflc` (the MFL compiler, built by the Go compiler) compiles its own source to
`mflc2`; `mflc2` compiles the source again to `mflc3`; assert
`binary(mflc2) == binary(mflc3)`. When that holds, machin compiles itself.
