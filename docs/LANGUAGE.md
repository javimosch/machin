# MFL Language Reference

MFL (Machine-First Language) is a small, statically-typed, Go-flavored language
that compiles to native code through C. This document is the canonical
reference for the language as currently implemented by the `machin` compiler.

## 1. Source format

An `.mfl` file **is** the program: it is base64, one function per line, with a
blank line between functions. There is no separate human-readable source of
truth. To author or inspect a program:

```sh
# Author: write loose Go-like text, then mint canonical MFL.
machin encode source.txt > program.mfl

# Run: compile to native and execute.
machin run program.mfl

# Build a standalone binary.
machin build program.mfl -o program

# Inspect the generated C without running.
machin build program.mfl --emit-c
```

Each non-blank line decodes to exactly one function declaration. The
human states intent; the machine reads and writes the encoded code.

## 2. Types

Types are inferred — you never write type annotations on parameters or
locals. The type system has:

| Type      | Literals            | Notes                                  |
|-----------|---------------------|----------------------------------------|
| `int`     | `42`, `-7`, `0`     | 64-bit signed integer                  |
| `float`   | `3.14`, `9.0`       | double precision; a decimal point makes a literal a float |
| `bool`    | `true`, `false`     | result of comparisons and `&&`/`\|\|`   |
| `string`  | `"hello"`           | immutable byte string                  |
| `[]T`     | `[]int{1, 2, 3}`    | growable slice of a single element type |

A literal with a decimal point (`9.0`) is a `float`; without one (`9`) it is an
`int`. Mixed `int`/`float` arithmetic promotes to `float` (e.g. `17.0 / 5`).

## 3. Functions

```go
func add(a, b) { return a + b }

func main() { println("3 + 4 =", add(3, 4)) }
```

- `main` is the entry point.
- Parameters and return types are inferred from use; a function is monomorphic
  (each parameter resolves to a single concrete type across the program).
- Recursion is supported (see `examples/complex/ackermann.mfl`).
- `return` with no value returns from a `void` function.

## 4. Statements

| Form                       | Meaning                                  |
|----------------------------|------------------------------------------|
| `x := expr`                | declare and initialize a new local       |
| `x = expr`                 | assign to an existing local              |
| `xs[i] = expr`             | assign into a slice element              |
| `if cond { ... }`          | conditional                              |
| `if cond { ... } else { ... }` | conditional with alternative         |
| `while cond { ... }`       | loop while `cond` holds                  |
| `for cond { ... }`         | identical to `while` (single-clause form)|
| `return expr`              | return a value                           |
| `go f(args)`               | run `f` concurrently as a goroutine      |

## 5. Operators

| Category    | Operators                          |
|-------------|------------------------------------|
| Arithmetic  | `+`  `-`  `*`  `/`  `%`             |
| Comparison  | `==`  `!=`  `<`  `<=`  `>`  `>=`    |
| Logical     | `&&`  `\|\|`                         |
| Unary       | `-` (negation)                     |

`%` (modulo) is integer-only. `+` also concatenates strings. Arithmetic
follows the usual precedence (`*`, `/`, `%` bind tighter than `+`, `-`), and
parentheses override it.

## 6. Builtins

| Builtin                     | Signature                       | Description                                  |
|-----------------------------|---------------------------------|----------------------------------------------|
| `print(...)`                | `(...) -> void`                 | print arguments space-separated, no newline  |
| `println(...)`              | `(...) -> void`                 | print arguments space-separated + newline    |
| `len(x)`                    | `(string\|[]T) -> int`          | length of a string or slice                  |
| `append(xs, v)`             | `([]T, T) -> []T`               | append an element, returning the grown slice |
| `str(x)`                    | `(int\|float) -> string`        | format a number as a string                  |
| `int(x)`                    | `(int\|float) -> int`           | truncate a number to an integer              |
| `sleep(ms)`                 | `(int) -> void`                 | sleep for the given milliseconds             |

### Networking builtins

| Builtin            | Signature              | Description                               |
|--------------------|------------------------|-------------------------------------------|
| `listen(port)`     | `(int) -> int`         | open a listening TCP socket, return its fd |
| `accept(fd)`       | `(int) -> int`         | accept a connection, return the client fd  |
| `read(fd)`         | `(int) -> string`      | read available bytes from a socket         |
| `write(fd, s)`     | `(int, string) -> int` | write a string to a socket                 |
| `close(fd)`        | `(int) -> void`        | close a socket                             |

## 7. Concurrency

`go f(args)` launches `f` on a new goroutine, mapped to a native thread in the
generated C (`-pthread`). The concurrent HTTP server
(`examples/complex/http_server.mfl`) accepts connections in a loop and handles
each on its own goroutine.

## 8. Compilation model

`machin` parses each function, infers types across the whole program, emits C,
and invokes the system C compiler (`cc -O2 -std=c11 -pthread`) to produce a
native binary. Override the compiler with the `CC` environment variable.
Because the output is ordinary optimized C, scalar MFL runs at C/Rust/Zig
speed.

## 9. Examples

The `examples/` tree is the best living documentation:

- `examples/basic/` — language fundamentals (variables, conditionals, loops,
  functions, arithmetic, the temperature conversion).
- `examples/complex/` — algorithms and feature showcases: recursion
  (`ackermann`, `fast_power`), number theory (`gcd_lcm`, `primes`,
  `perfect_numbers`, `collatz`), slices (`slices`, `stats`), integer routines
  (`digit_ops`, `to_binary`, `isqrt`), floats (`pi_leibniz`,
  `compound_interest`), and concurrency (`goroutines`, `http_server`).

Run them all with `examples/run.sh` (or `make examples`), which auto-discovers
every `*.mfl` file.
