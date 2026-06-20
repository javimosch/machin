# MFL Compiler Architecture

`machin` compiles **MFL** (Machine-First Language) to a native binary by
emitting C and handing it to the system C compiler. MFL is "machine-first":
the source of truth is base64 — one function per line, a blank line between
functions — so there is no human-readable source file and no "decode" step
in the usual sense. The human states intent; the machine reads and writes
the canonical base64.

This document traces a program from `.mfl` bytes to a running native binary
and points at the source file that owns each stage.

## Pipeline overview

```
  .mfl  (base64, 1 func/line)
    │  loadMFL            main.go      decode base64 → readable func text
    ▼
  Lex                     lexer.go     text → []Token
    │
    ▼
  ParseFunc               parser.go    tokens → *FuncDecl  (AST nodes: ast.go)
    │
    ▼
  Check                   types.go     type inference by unification
    │
    ▼
  CompileToC              codegen.go   typed AST → C source
    │
    ▼
  BuildBinary             build.go     cc -O2 -std=c11 -pthread → native binary
    │
    ▼
  native executable       runs at C/Rust/Zig speed for scalar work
```

## Stages

### 1. Load & decode — `main.go` (`loadMFL`)
Reads the `.mfl` file, splits it into non-empty lines, and base64-decodes
each line back into a single function's readable text. The `encode` command
(`cmdEncode`) is the inverse: it lifts loose Go-like text into canonical MFL
via `splitFunctions` + `normalize` and re-emits the base64.

### 2. Lexing — `lexer.go` (`Lex`)
Turns function text into a token stream. Recognized punctuation and operators
include `( ) { } , ; [ ]`, the arithmetic/comparison set `+ - * / % < > ! =`,
and the two-character tokens `== != <= >= && ||`. String escapes (`\n`, `\r`,
`\t`, `\"`, `\\`) are handled here.

### 3. Parsing — `parser.go` (`ParseFunc`) + `ast.go`
Builds one `*FuncDecl` per function. AST node types (declarations,
statements, expressions) are defined in `ast.go`. MFL supports `func`,
`if`/`else`, `while`, `for`, `return`, short declarations (`:=`),
assignment, calls, slices, and `go` (goroutines).

### 4. Type inference — `types.go` (`Check`)
Statically types the whole program by **unification** — types are inferred,
not annotated. Numeric literals start as `int` and can unify to `float` when
combined with a float (e.g. `2.0 * k`). Type errors are reported here, at
compile time, instead of surfacing as runtime surprises.

### 5. C emission — `codegen.go` (`CompileToC`)
Walks the typed AST and emits portable C11. Builtins such as `println`,
`print`, `len`, `append`, `str`, and `int`, plus the networking/goroutine
runtime, are lowered to C here.

### 6. Native build — `build.go` (`BuildBinary`, `RunCaptured`)
Writes the generated C to a temp file and invokes the C compiler:

```
cc -O2 -std=c11 -pthread -o <out> <generated.c>
```

`BuildBinary` produces a standalone executable; `RunCaptured` builds to a
temp binary, runs it, and returns captured stdout (used throughout the test
suite). The `cc` binary is resolved by `ccPath()`.

## Where to look when changing things

| You want to…                          | Edit…        |
|---------------------------------------|--------------|
| add a token or operator               | `lexer.go`   |
| add syntax / a new statement form     | `parser.go`, `ast.go` |
| change typing rules / add inference   | `types.go`   |
| change generated C / add a builtin    | `codegen.go` |
| change compiler flags / build steps   | `build.go`   |
| add a CLI subcommand                  | `main.go`    |

## Running

```sh
go build -o machin .
./machin run   examples/complex/factorial.mfl   # compile + run
./machin build examples/complex/factorial.mfl    # emit a native binary
./machin encode src.txt > prog.mfl               # text → canonical MFL
```
