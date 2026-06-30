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

### Stage 2 — parser ✅ DONE
The full parser is ported and verified. `selfhost/parse.src`.
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

**Top-level decls + program driver — done.** `parseTypeDecl`, `parseExternDecl`
(header/link/cflags/cstruct/fn, incl. `*T`/`T*` FFI marshaling prefixes/suffixes),
`ParseGlobal`, and `parseTypeName` (`[]T`, `map[K]V`, `chan T`, `func`). The
`--program <file.mfl>` mode classifies every decl line (two-pass: `type`+`cstruct`
names first), parses it, and dumps in source order. **Verified across 39 complete
programs: 928 declarations (50 types, 29 externs, 128 globals, 721 funcs), 0
mismatches** — every AST byte-identical to the Go parser, including the parser
parsing its own source.

Not replicated (correctly): `ParseProgram`'s cstruct→TypeDecl synthesis and
`liftClosures` are post-parse *transforms*, not parsing — they belong to later
stages. The parser milestone compares each decl as-parsed, which is exact.

### Stage 3 — typecheck / inference (in progress)
Port `types.go` (~2700 LOC) — the largest, most semantic stage. Architecture
(learned while building the oracle):

- **Union-find inference.** Every expr/var gets a *slot*; `union` merges slots and
  `reconcile` merges `Kind`s (KVar/KNum/KInt/KFloat/KBool/KString/KVoid/KSlice/
  KStruct/KChan/KMap/KFunc/KBytes). `KNum` is a flexible numeric literal that
  finalizes to `int` unless unified with `float`.
- **Constraint generation** (`genExpr`/`genStmt`): literals → kind slots, `+` → a
  *deferred* plus-constraint (numeric vs string concat), `-*/`→ same-numeric,
  `%&|^<<>>`→ int, comparisons→bool, index/field/range→deferred uses, resolved to a
  fixpoint in `solve`/`resolveDeferred`, then `finalizeMono` (`KNum`→`int`).
- **Monomorphization.** A function with params is a *generic template*; each call
  `instantiate`s a fresh specialization (`name$N`), so `dbl` used at int and float
  yields two instances. Locals/params/returns are keyed by **instance**, not source
  name. Verified: the oracle dumps two `dbl` instances for a mixed-type program.
- **The builtin table** (`genCall`, ~800 LOC) hard-codes every builtin's signature.

**Done: the oracle.** `machin checktest --program <file.mfl>` runs the Go checker and
dumps, per monomorphized instance, `(func NAME (param p KIND) (ret i KIND) (local l
KIND) …)`, keyed by `source|signature` and sorted — deterministic, independent of the
instantiation counter (so the MFL port need not reproduce `$N` names). Validated on
monomorphic, multi-function, and mixed-int/float (two-instance) programs.

**Sub-plan for the MFL checker** (`selfhost/check.src`), each slice oracle-verified:
1. ✅ **engine: slot arrays + `find`/`union`/`reconcile`.** DONE. `selfhost/check.src`
   ports the parallel-array `Checker` (parent/kind/elem/sname/mkey/mval/fsig + a `Sig`
   table), `find` (path-halving), `reconcile`, and `union` with all recursive
   sub-unions (slice/chan elem, map key/val, func params+ret) and the struct-name /
   func-arity mismatch checks. Oracle `machin uftest <script>` drives the *real* Go
   engine with a scripted op sequence (`var|int|slice E|map K V|func P0,P1|R|union A B|
   dump`) and dumps the canonical slot table; the MFL engine runs the identical script.
   **Verified byte-identical: 6 edge cases + 520 randomized fuzz scripts (to 300 slots /
   500 unions).** `selfhost/verify-check.sh`, `selfhost/gen-uf.py`. MFL global-state
   note: package globals need initializers (`var g = []int{}`), then `append` +
   index-assignment mutate them in place — the natural fit for the array-based engine.
2. ✅ **constraint generation (non-call grammar) + solve + defaults.** DONE.
   `checkgen.src` ports `genExpr`/`genBinary`/`genStmt` for literals, idents, unary,
   all binary ops (incl. the deferred `+` numeric-vs-concat constraint), `:=`/`=`,
   if/while/return/arena/break/continue, and slice literals; plus `solve` (pair
   unification + `+`-fixpoint) and the `KNum`/`KVar`→`int` defaults. Verified on
   MAIN-ONLY programs via `checktest --program`: 3 hand cases (every operator, the
   int→float flex back-prop, control flow, slices) + **700 random type-correct
   programs, 0 mismatches**. `verify-check2.sh` + `gen-check.py`. Module split done so
   one `main` per binary: `parsemain.src` (parser CLI), `check.src` (engine only),
   `ufmain.src` (uftest), `checkgen.src`+`checkmain.src` (the checker). The integrated
   checker = `lex+parse+check+checkgen+checkmain` → `mfl-check`. Unsupported nodes emit
   `(unsupported)` so the harness skips out-of-slice programs.
2b. ✅ **deferred grammar + resolveDeferred.** DONE. `checkgen.src` adds index `x[i]`,
   field `x.f`, `for…range` (slice/string/map/chan), `make(map[K]V)`, `make(chan T)`,
   `<-ch`, `ch <- v`, struct literals (named + positional) and field/index assignment —
   plus `tryIndex`/`tryRange`/field resolution and the `resolveDeferred` fixpoint
   (resolve what now has a known base kind → re-solve → repeat). A struct registry
   (`type` decls → field names/types) backs field access + struct literals; `type_slot`
   now parses `[]T`/`chan T`/`map[K]V`/struct names. Verified vs `checktest --program`:
   a comprehensive hand battery (maps, structs both forms, channels, field/index ops) +
   **800 random type-correct programs (slices, indexing, range), 0 mismatches**.
3. user-function calls + `instantiate` (monomorphization) → multi-function programs.
4. the builtin signature table (the long tail) → full corpus parity.

### Stage 4 — C codegen, Stage 5 — driver, Stage 6 — fixpoint
(unchanged; see top.)

### Stages 3–5 — typecheck, codegen, driver
Same oracle-diff discipline. Codegen verifies two ways: diff the emitted C against
Go codegen, **and** compile it + run the existing test corpus.

### Stage 6 — fixpoint (self-hosting achieved)
`mflc` (the MFL compiler, built by the Go compiler) compiles its own source to
`mflc2`; `mflc2` compiles the source again to `mflc3`; assert
`binary(mflc2) == binary(mflc3)`. When that holds, machin compiles itself.
