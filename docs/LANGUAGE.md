# MFL Language Reference

A consolidated reference for the MFL language: types, operators, control flow,
slices, functions, and built-ins. MFL is statically typed by inference and
compiles to native code through C, so a program runs at C/Rust/Zig speed with no
runtime or garbage collector.

> A `.mfl` file is base64, one function per line, with a blank line between
> functions. The base64 *is* the program — there is no separate human-readable
> source of truth. The readable form shown throughout this document is what each
> line decodes to. See [`docs/CLI.md`](CLI.md) for the encode/run/build commands.

## Types

Types are never written down in user code — they are inferred. The primitive
types the inferencer works with are:

| Type     | Literals / origin                       | Example            |
|----------|-----------------------------------------|--------------------|
| `int`    | integer literals, integer arithmetic    | `42`, `n % 10`     |
| `float`  | a literal with a `.`, or float arithmetic | `3.14`, `7.0 / 2.0` |
| `bool`   | `true`, `false`, comparisons, `!`       | `n < 2`, `!ok`     |
| `string` | double-quoted text                      | `"hello"`          |
| `[]int`  | slice literals and `append`             | `[]int{1, 2, 3}`   |

Inference unifies a variable's type across its uses. A variable that starts as
`int` becomes `float` if it is ever combined with a float:

```go
func main() { k := 0 println(7 / 2, 2.0 * k + 7.0 / 2.0) }
// 3 3.5  -- the first division is integer, the second unifies to float
```

## Variables

```go
x := 10        // declare and infer
x = x + 1      // assign (variable must already exist)
```

`:=` introduces a new binding; `=` reassigns an existing one.

## Operators

| Category   | Operators                            |
|------------|--------------------------------------|
| Arithmetic | `+`  `-`  `*`  `/`  `%`               |
| Comparison | `==`  `!=`  `<`  `<=`  `>`  `>=`      |
| Logical    | `&&`  `||`  `!`                       |

`+` also concatenates strings. `%` is integer-only (applying it to floats is a
compile-time type error). Integer `/` truncates; float `/` is real division.

## Control flow

```go
if n < 2 { return n }

if x > 0 { println("pos") } else if x == 0 { println("zero") } else { println("neg") }

while i <= n { sum = sum + i i = i + 1 }   // condition loop

for i < len(xs) { ... i = i + 1 }          // for with a condition (same as while)

for { conn := accept(server) ... }          // bare for is an infinite loop
```

## Functions

```go
func gcd(a, b) { while b != 0 { t := b b = a % b a = t } return a }
func main() { println(gcd(48, 36)) }
```

Parameter and return types are inferred from how the function is called and what
it returns. Functions may recurse:

```go
func fib(n) { if n < 2 { return n } return fib(n-1) + fib(n-2) }
```

## Slices

```go
xs := []int{1, 2, 3}     // slice literal
xs = append(xs, 4)       // grow (returns the new slice)
xs[0] = 10               // index assignment
v := xs[2]               // index read
n := len(xs)             // length
```

Slices can be passed to functions; the element type is inferred from the call
site:

```go
func sum(xs) { s := 0 i := 0 while i < len(xs) { s = s + xs[i] i = i + 1 } return s }
func main() { println(sum([]int{1, 2, 3, 4})) }   // 10
```

## Built-in functions

| Built-in            | Purpose                                            |
|---------------------|----------------------------------------------------|
| `print(a, b, ...)`  | print arguments separated by spaces, no newline    |
| `println(a, ...)`   | like `print` but adds a trailing newline           |
| `str(x)`            | convert an `int` to its decimal `string`           |
| `len(x)`            | length of a `string` or slice                      |
| `append(xs, v)`     | return `xs` with `v` appended                      |
| `sleep(ms)`         | pause the current goroutine for `ms` milliseconds  |

The networking built-ins (`listen`, `accept`, `read`, `write`, `close`) and the
`go` keyword for goroutines are documented in [`docs/NETWORKING.md`](NETWORKING.md).

## Comments

```go
// line comments run to the end of the line
```

## Worked example

```go
func is_prime(n) { if n < 2 { return false } i := 2 while i * i <= n { if n % i == 0 { return false } i = i + 1 } return true }
func sum_primes_below(n) { s := 0 i := 2 while i < n { if is_prime(i) { s = s + i } i = i + 1 } return s }
func main() { println(sum_primes_below(100)) }   // 1060
```

See [`examples/complex/`](../examples/complex) for many runnable programs.
