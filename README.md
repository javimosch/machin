# machin — MFL (Machine-First Language)

A small backend language **based on Go but machine-first**. Source programs are
stored as **base64**: one function per line, a blank line between functions. The
machine reads the dense `.mfl` form directly; humans author a readable `.mfs`
form and let the toolchain encode it.

```
ZnVuYyBmaWIobikgeyBpZiBuIDwgMiB7IHJldHVybiBuIH0gcmV0dXJuIGZpYihuIC0gMSkgKyBmaWIobiAtIDIpIH0=

ZnVuYyBtYWluKCkgeyBwcmludGxuKGZpYigxMCkpIH0=
```

## Build

```sh
go build -o machin .
```

## Toolchain

| Command | Description |
|---------|-------------|
| `machin run <file.mfl>`    | base64-decode each line, parse, execute `main` |
| `machin encode <file.mfs>` | compile readable source → machine-first `.mfl` (stdout) |
| `machin decode <file.mfl>` | expand machine-first `.mfl` → readable source (stdout) |

## Quick start

```sh
./machin encode examples/demo.mfs > examples/demo.mfl
./machin run examples/demo.mfl
```

## Language (the readable `.mfs` form)

Go-flavored, deliberately minimal:

- **Functions:** `func name(a, b) { ... }` — last `return` yields a value.
- **Values:** int64, float64, string, bool, nil. `/` of two ints is integer
  division; mixing in a float promotes to float.
- **Variables:** `x := expr` (declare), `x = expr` (assign). `var x = expr` also works.
- **Control flow:** `if/else if/else`, `while cond { ... }`.
- **Operators:** `+ - * / %`, `== != < <= > >=`, `&& || !`. `+` concatenates strings.
- **Builtins:** `print`, `println`, `len(s)`, `str(v)`, `int(v)`.

Comments (`// ...`) and multi-line layout are allowed in `.mfs`; `encode`
strips and flattens each function to a single canonical line before base64.

## Why machine-first?

The canonical on-disk unit is the base64 line, not the glyphs. Diffs, transport,
and storage operate on opaque one-line-per-function records; the readable form is
a *projection* produced on demand by `decode`. Functions are independently
addressable units — one line each — so tooling can ship, cache, or rewrite a
single function without touching the rest of the file.

## Layout

- `lexer.go` — tokenizer
- `ast.go` — node definitions
- `parser.go` — precedence-climbing parser
- `interp.go` — tree-walking evaluator
- `builtins.go` — built-in functions
- `pretty.go` — AST → readable source (for `decode`)
- `main.go` — CLI + `.mfl`/`.mfs` encode/decode
- `mfl_test.go` — tests
