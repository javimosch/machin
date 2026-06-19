# machin — MFL (Machine-First Language)

A minimal backend language **based on Go but machine-first**. A program **is**
base64: one function per line, a blank line between functions. There is no
human-readable source of truth and no "decode" step — the `.mfl` is the program.
The human states intent; the machine reads and writes the code.

MFL is **statically typed by inference** (no annotations) and **compiles to
native code through C**, so it runs at C/Rust/Zig speed.

```
ZnVuYyBmaWIobikgeyBpZiBuIDwgMiB7IHJldHVybiBuIH0gcmV0dXJuIGZpYihuIC0gMSkgKyBmaWIobiAtIDIpIH0=

ZnVuYyBtYWluKCkgeyBwcmludGxuKGZpYig0MCkpIH0=
```

## Performance

`fib(40)` — naive recursion, ~331M calls:

| implementation | time |
|----------------|------|
| **MFL** (native, `cc -O2`)   | **0.20s** |
| hand-written C (`cc -O2`)    | 0.19s |
| Rust (`rustc -O`)            | 0.29s |

MFL emits C and hands it to the system optimizer, so it lands on hand-written C —
the same class of machine code as Rust/Zig for scalar work.

## Build the compiler

```sh
go build -o machin .
```

Requires a C compiler on `PATH` (`cc`/`gcc`/`clang`; override with `$CC`).

## Toolchain

| Command | Description |
|---------|-------------|
| `machin run <file.mfl>`           | compile to native + execute |
| `machin build <file.mfl> [-o out]`| compile to a native binary |
| `machin build <file.mfl> --emit-c`| print the generated C and stop |
| `machin encode <src>`             | machine tool: mint MFL from loose Go-like text |

There is no `decode`. Reading MFL is the machine's job, not a step in the flow.

```sh
./machin run examples/demo.mfl
./machin build examples/complex/primes.mfl -o primes && ./primes
```

## Language

Go-flavored, deliberately minimal. The decoded form of each base64 line obeys:

- **Functions:** `func name(a, b) { ... }`. Types are inferred from use.
- **Types:** `int` (int64), `float` (double), `bool`, `string`, and slices
  `[]T`. An integer literal is `int` unless it meets a float, then the value is
  `float`. `/` of two ints is integer division.
- **Slices:** literals `[]int{1, 2, 3}` / `[]string{}`, indexing `s[i]` (read and
  assign), `len(s)`, and `append(s, x)` (returns the grown slice, Go-style).
  A slice is a header `{data, len, cap}` over an unboxed backing array.
- **Variables:** `x := expr` (declare), `x = expr` (assign), `var x = expr`.
- **Control flow:** `if / else if / else`; Go-style loops `for cond { ... }` and
  bare `for { ... }` (infinite). `while cond { ... }` is also accepted.
  Conditions must be `bool`.
- **Concurrency:** `go f(args)` spawns a goroutine (backed by a pthread).
  `sleep(ms)` pauses.
- **Operators:** `+ - * / %`, `== != < <= > >=`, `&& || !`. `+` concatenates
  when its operands are strings.
- **Builtins:** `print`, `println`, `len(x)`, `str(n)`, `int(x)`, `append(s, x)`,
  `sleep(ms)`.
- **Networking** (the low-level shape of Go's `net` package):
  `listen(port) → fd`, `accept(fd) → conn`, `read(conn) → string`,
  `write(conn, s) → n`, `close(conn)`.

A type clash (e.g. assigning a string to an int variable) is a compile error.
Types are inferred even through slices: `func first(xs) { return xs[0] }` works
on `[]string` or `[]int` depending on the call site.

```sh
machin run examples/complex/http_server.mfl   # then open the printed URL
```

## How it works

```
.mfl ──base64 decode──▶ parse ──▶ infer types ──▶ emit C ──▶ cc -O2 ──▶ native binary
```

- Type inference is unification over a union-find (`types.go`): every parameter,
  local, return, and expression gets a slot; constraints merge them; unresolved
  numeric slots default to `int`.
- The C backend (`codegen.go`) emits one C function per MFL function with no
  boxing — `int64_t`/`double`/`char*` directly — so the optimizer sees ordinary
  C.

## Layout

- `lexer.go` — tokenizer
- `ast.go` — node definitions
- `parser.go` — precedence-climbing parser
- `types.go` — type inference (unification)
- `codegen.go` — C backend
- `build.go` — invokes `cc`, runs binaries
- `main.go` — CLI + `.mfl` loading
- `mfl_test.go` — tests (compile + run natively)
- `examples/` — programs as `.mfl`; `examples/run.sh` runs them all
