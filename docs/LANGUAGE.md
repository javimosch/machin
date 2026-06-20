# MFL Language Reference

MFL (Machine-First Language) is a statically-typed, Go-flavored language whose
on-disk form is **base64** — one function per line, a blank line between
functions. It compiles to native code through C (`cc -O2`).

This document describes the *decoded* surface syntax. Remember: on disk, each
function below is a single base64 line (see [Encoding](#encoding)).

---

## Encoding

A `.mfl` file is a sequence of base64-encoded function definitions:

- **One function per line.** Each line is the base64 of exactly one `func ...`.
- **A blank line separates functions.**
- There is no human-readable source of truth — the base64 *is* the program.

```bash
# Decode a program to inspect it:
while read -r line; do [ -n "$line" ] && echo "$line" | base64 -d && echo; done < examples/demo.mfl
```

---

## Types

Types are inferred by unification — there are **no type annotations**.

| Type      | Literals / construction        | Notes                                  |
|-----------|--------------------------------|----------------------------------------|
| `int`     | `0`, `42`, `-7`                | 64-bit signed integer                  |
| `float`   | `3.14`, `0.5`                  | double-precision                       |
| `string`  | `"hello"`                      | concatenate with `+`                   |
| `bool`    | `true`, `false`                | produced by comparisons                |
| `[]int`   | `[]int{}`, `[]int{1, 2, 3}`    | grow with `append`, index with `xs[i]` |

Each value's type is determined by how it is used; mixing incompatible types is
a **compile-time error**, not a runtime surprise.

---

## Functions

```go
func add(a, b) { return a + b }

func main() { println(add(2, 3)) }
```

- Parameters are untyped in the surface syntax; their types are inferred from
  use at call sites.
- `main` is the program entry point.
- A function returning a value uses `return expr`; a function used only for its
  side effects may omit `return`.

---

## Variables

```go
x := 10      // declare + infer type
x = x + 1    // reassign (type must match)
```

- `:=` declares a new variable and infers its type from the initializer.
- `=` reassigns an existing variable.

---

## Control flow

```go
if n % 15 == 0 { println("FizzBuzz") } else if n % 3 == 0 { println("Fizz") } else { println(n) }

while i <= n { acc = acc * i  i = i + 1 }

for i < len(xs) { total = total + xs[i]  i = i + 1 }
```

- `if` / `else if` / `else` — conditions are `bool` expressions.
- `while cond { ... }` — loops while `cond` holds.
- `for cond { ... }` — equivalent condition-only loop (Go-style `for`).

---

## Operators

| Category    | Operators                          |
|-------------|------------------------------------|
| Arithmetic  | `+`  `-`  `*`  `/`  `%`             |
| Comparison  | `==`  `!=`  `<`  `<=`  `>`  `>=`    |
| String      | `+` (concatenation)                |

`%` is integer-only. `/` on `int` is integer division.

---

## Slices

```go
xs := []int{}          // empty
xs = append(xs, 42)    // grow
v := xs[0]             // index (0-based)
xs[1] = 7              // assign element
n := len(xs)           // length
```

`len` also returns the length of a `string`.

---

## Builtins

| Builtin                     | Purpose                                      |
|-----------------------------|----------------------------------------------|
| `print(...)`                | print arguments without a trailing newline   |
| `println(...)`              | print arguments followed by a newline        |
| `len(x)`                    | length of a slice or string                  |
| `append(xs, v)`             | return `xs` with `v` appended                |
| `str(n)`                    | convert an `int` or `float` to its `string`  |
| `int(n)`                    | convert a numeric value to `int` (truncates) |
| `sleep(ms)`                 | suspend the current goroutine (milliseconds) |
| `listen(port)`              | open a TCP listening socket                  |
| `accept(fd)`                | accept a connection, return its socket fd    |
| `read(fd)` / `write(fd, s)` | read from / write to a socket                |
| `close(fd)`                 | close a socket                               |

---

## Concurrency

```go
go handle(conn)   // run handle(conn) in a goroutine
```

`go` launches a function call concurrently. `sleep(ms)` suspends the current
goroutine for the given number of milliseconds. Combined with the networking
builtins, these enable the concurrent HTTP server in
`examples/complex/http_server.mfl`.

---

## See also

- [`../README.md`](../README.md) — project overview and the toolchain
- [`../examples/`](../examples/) — runnable programs (`machin run <file>.mfl`)
- `machin build <file>.mfl --emit-c` — inspect the C the compiler emits
