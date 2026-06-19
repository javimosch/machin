# machin — MFL (Machine-First Language)

A small backend language **based on Go but machine-first**. A program **is**
base64: one function per line, a blank line between functions. There is no
human-readable source of truth — the `.mfl` is the program. The human states
intent; the machine reads and writes the code. Reading the dense form is the
machine's job, not a step the human is expected to perform.

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
| `machin run <file.mfl>`    | base64-decode each line, parse, execute `main` — the normal path |
| `machin decode <file.mfl>` | render MFL as readable text — a human inspection escape-hatch |
| `machin encode <text>`     | machine convenience: lift loose Go-like text into canonical MFL |

`run` is the language. `decode` exists only so a human *can* peek; it is not part
of authoring. `encode` is a tool the machine uses to mint MFL from scratch text —
the human never has to touch it.

## Quick start

```sh
./machin run examples/demo.mfl       # run a program
./machin decode examples/demo.mfl    # (optional) look at what it says
```

## Language

Go-flavored, deliberately minimal. The decoded form of each line obeys:

- **Functions:** `func name(a, b) { ... }` — last `return` yields a value.
- **Values:** int64, float64, string, bool, nil. `/` of two ints is integer
  division; mixing in a float promotes to float.
- **Variables:** `x := expr` (declare), `x = expr` (assign). `var x = expr` also works.
- **Control flow:** `if/else if/else`, `while cond { ... }`.
- **Operators:** `+ - * / %`, `== != < <= > >=`, `&& || !`. `+` concatenates strings.
- **Builtins:** `print`, `println`, `len(s)`, `str(v)`, `int(v)`.

## Why machine-first?

The canonical unit is the base64 line, not the glyphs. The human delegates
reading and writing of code to the machine and works only in intent; the machine
emits and consumes MFL directly. Diffs, transport, and storage operate on opaque
one-line-per-function records. Functions are independently addressable units —
one line each — so tooling can ship, cache, or rewrite a single function without
touching the rest of the file. The readable rendering is something `decode`
produces on demand when a human chooses to look; it is never the source.

## Layout

- `lexer.go` — tokenizer
- `ast.go` — node definitions
- `parser.go` — precedence-climbing parser
- `interp.go` — tree-walking evaluator
- `builtins.go` — built-in functions
- `pretty.go` — AST → readable source (for `decode`)
- `main.go` — CLI + `.mfl`/`.mfs` encode/decode
- `mfl_test.go` — tests
