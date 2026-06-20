# The MFL Language

MFL is a small, statically typed language that compiles to native code through
C. Types are **inferred** — there are no type annotations — but every program is
fully type-checked before any C is emitted, so type errors are reported at
compile time rather than as raw `cc` failures or runtime crashes.

A `.mfl` file is the canonical source of truth. Each function is stored as one
base64-encoded line; use `machin encode <source.txt>` to turn readable,
Go-flavored text into a `.mfl` file (the encoder also type-checks), and
`machin run <file.mfl>` / `machin build <file.mfl> -o <bin>` to execute or
compile it.

## Types

| Kind     | Notes                                                              |
|----------|-------------------------------------------------------------------|
| `int`    | 64-bit signed integer (the default for numeric literals).         |
| `float`  | 64-bit IEEE double. A numeric literal becomes `float` on contact with a float. |
| `bool`   | `true` / `false`.                                                 |
| `string` | Immutable text; `+` concatenates.                                 |
| `[]T`    | Slice of element type `T`, e.g. `[]int{1, 2, 3}`.                  |

Numeric literals start as a flexible numeric kind: they default to `int` but
unify "up" to `float` the moment they meet a float (so `2.0 * k` makes `k` a
float).

## Operators

| Operators           | Operand types        | Result   | Notes |
|---------------------|----------------------|----------|-------|
| `+`                 | numeric **or** string| same     | Numeric addition or string concatenation. |
| `-` `*` `/`         | numeric (int/float)  | same     | |
| `%`                 | **int only**         | int      | C's `%` is integer-only; float operands are a compile-time type error. |
| `==` `!=`           | matching types       | bool     | Strings compare **by value** (content), not by pointer. |
| `<` `<=` `>` `>=`   | matching types       | bool     | |
| `&&` `||`           | bool                 | bool     | |
| `!`                 | bool                 | bool     | |

## Builtins

| Call                  | Argument types        | Returns  |
|-----------------------|-----------------------|----------|
| `print(...)`          | any                   | —        |
| `println(...)`        | any                   | —        |
| `len(x)`              | **string or slice**   | int      |
| `append(xs, v)`       | `[]T`, `T`            | `[]T`    |
| `str(n)`              | numeric               | string   |
| `int(n)`              | numeric               | int      |
| `sleep(ms)`           | int                   | —        |
| `listen(port)`        | int                   | int (fd) |
| `accept(fd)`          | int                   | int (fd) |
| `read(fd)`            | int                   | string   |
| `write(fd, s)`        | int, string           | —        |
| `close(fd)`           | int                   | —        |

`len` is rejected at compile time on anything other than a string or slice —
calling it on an `int` is a type error, not silently `strlen()` on a number.

## Control flow

```go
if cond { ... } else { ... }
while cond { ... }
for cond { ... }          // alias for while
return expr
go f(args)                // run a user function concurrently (goroutine)
```

## Declarations

```go
x := 10                   // infer and declare
var y = 5                 // same, explicit keyword
x = x + y                 // assign to an existing variable
```

Functions take untyped parameters; their types are inferred from how they are
used and from their call sites:

```go
func gcd(a, b) {
	while b != 0 { t := b  b = a % b  a = t }
	return a
}
```

See `examples/` for runnable programs covering recursion, slices, goroutines,
a concurrent HTTP server, and a range of numeric algorithms.
