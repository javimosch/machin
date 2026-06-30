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

**Done: expressions + statements + function declarations.** The full expression
grammar (`parseExpr`..`parsePrimary`, precedence climbing, calls/spread/index/field
chains/call-value/slice+struct literals/make/unary/recv), every statement
(`parseStmt`/`parseBlock`/if-else/while/for/range/select/multi-assign/send/go/arena/
var/break/continue/index+field assign), funclit bodies, and `parseFuncDecl`
(params/variadic/named-returns/export). Oracle modes: `--expr`, `--func`, `--funcs
<file.mfl>` (every function in a program, with a two-pass known-struct scan over
`type`+`cstruct` names).

**Verified at scale: 39 complete programs (the self-host compiler, raylib 3D demos,
games, tools), 721 functions, 0 mismatches, 0 parse-errors** — every function's AST
is byte-identical to the Go parser's. Includes the parser parsing its own source.
`selfhost/verify-parse.sh` (repo-only) covers a 25-expr battery + self-application.
No new builtin needed.

Robustness: the MFL parser tracks `g_err` and every consuming loop has a
no-progress backstop, so malformed input reports an error (matching the oracle's
`(parse-error)`) instead of hanging — a property a real compiler needs.

Float literals: the oracle dumps `FormatFloat(v,'f',-1,64)` (shortest decimal, no
exponent) and the MFL side strips trailing zeros from the lexeme — they match
exactly on all real source (sidesteps `%v`'s `1e-05`/`1e+21`).

**Still to port for Stage 2:** the other top-level decls — `parseTypeDecl`,
`parseExternDecl`, `ParseGlobal` — and the whole-program driver (`ParseProgram`:
classify decls, types/cstructs first to seed struct names, then funcs) for full
`.mfl`-program AST parity. The function parser (the bulk) is done.

### Stages 3–5 — typecheck, codegen, driver
Same oracle-diff discipline. Codegen verifies two ways: diff the emitted C against
Go codegen, **and** compile it + run the existing test corpus.

### Stage 6 — fixpoint (self-hosting achieved)
`mflc` (the MFL compiler, built by the Go compiler) compiles its own source to
`mflc2`; `mflc2` compiles the source again to `mflc3`; assert
`binary(mflc2) == binary(mflc3)`. When that holds, machin compiles itself.
