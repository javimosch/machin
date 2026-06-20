# MFL Semantics

This document describes the static-typing and operator rules of the
Machine-First Language (MFL). It complements the example programs under
`examples/` and the language overview in the project `README.md`.

MFL has **no type annotations**. Every variable, parameter, and return
type is inferred. A program that does not type-check is a build failure —
type clashes are reported at compile time, not at runtime.

## Types

| Type      | Literal examples        | Notes                                  |
|-----------|-------------------------|----------------------------------------|
| `int`     | `0`, `42`, `-7`         | 64-bit signed integer                  |
| `float`   | `0.0`, `3.14`, `-1.5`   | double precision; written with a `.`   |
| `bool`    | `true`, `false`         | result of comparisons / logic          |
| `string`  | `"hi"`                  | immutable text                         |
| `[]T`     | `[]int{1, 2, 3}`        | slice of a single element type `T`     |

A literal containing a decimal point (e.g. `2.0`) is a `float`; a bare
integer literal (e.g. `2`) is an `int`. There is no implicit conversion
between `int` and `float` at the *variable* level — `x := 1` makes `x` an
`int` for its whole lifetime.

## Numeric operators

The binary arithmetic operators are `+`, `-`, `*`, `/`, and `%`.

- `+`, `-`, `*`, `/` apply to `int` and `float`.
- Within a single expression, an `int` operand is promoted to `float`
  when combined with a `float`, so `1.0 / i` (where `i` is an `int`)
  evaluates as floating-point division. The promotion is per-expression;
  it does not change the declared type of `i`.
- `%` (modulo) is defined for **`int` operands only**. C's `%` is
  integer-only, so applying it to floats is a type error.

`+` is additionally defined on `string` operands, where it concatenates.

### Comparison and equality

`==`, `!=`, `<`, `<=`, `>`, `>=` produce a `bool`.

- Ordering (`<`, `<=`, `>`, `>=`) applies to `int` and `float`.
- `==` / `!=` apply to `int`, `float`, `bool`, and `string`. **String
  equality is by value** — two strings compare equal when their contents
  are equal, regardless of how each was built (`"ab" == "a" + "b"` is
  `true`).

## `len`

`len(x)` returns an `int`. Its argument must be a `string` or a slice
(`[]T`):

- on a `string`, `len` is the number of bytes;
- on a slice, `len` is the number of elements.

Calling `len` on an `int`, `float`, or `bool` is a type error.

## Control flow

`if` / `else`, `for`, and `while` conditions must be `bool`. MFL does not
treat integers as truthy; write `n != 0`, not `n`.

## Known limitations

The following are tracked issues where the current compiler diverges from
the semantics above. They are documented here so the gap is explicit:

- **String `==` / `!=`** currently compile to a pointer comparison rather
  than a value comparison (issue #1).
- **`%` on `float`** is accepted by the checker and leaks a raw C compiler
  error instead of a clean MFL type error (issue #2).
- **`len` on a non-slice / non-string** value is not rejected and falls
  through to `strlen` on a non-pointer (issue #3).
- Passing a slice element to a function whose return type is `float`
  (e.g. `harmonic(ns[i])` where `ns` is `[]int`) can fail inference with
  `type mismatch: float vs int`; calling with literal arguments works.
