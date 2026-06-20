# MFL Operators

This is a catalogue of the operators MFL supports. Remember that `.mfl`
source is base64 (one function per line); the snippets below show the
*decoded* form for readability.

## Arithmetic

| Operator | Meaning        | Operand types      |
|----------|----------------|--------------------|
| `+`      | add            | int, float, string (string `+` concatenates) |
| `-`      | subtract       | int, float         |
| `*`      | multiply       | int, float         |
| `/`      | divide         | int, float         |
| `%`      | modulo         | int                |

## Comparison

| Operator | Meaning            |
|----------|--------------------|
| `==`     | equal              |
| `!=`     | not equal          |
| `<`      | less than          |
| `<=`     | less than or equal |
| `>`      | greater than       |
| `>=`     | greater or equal   |

## Logical

| Operator | Meaning |
|----------|---------|
| `&&`     | and     |
| `\|\|`   | or      |
| `!`      | not     |

## Assignment

| Operator | Meaning                       |
|----------|-------------------------------|
| `:=`     | declare and initialise        |
| `=`      | assign to an existing lvalue  |

## Compound assignment & increment

These are **pure syntactic sugar**: the parser desugars them into an
ordinary assignment over the existing binary operators, so they typecheck
and compile exactly like the long form. They apply to any assignable
lvalue — a plain variable or a slice element (`xs[i]`).

| Operator | Desugars to   |
|----------|---------------|
| `x += e` | `x = x + e`   |
| `x -= e` | `x = x - e`   |
| `x *= e` | `x = x * e`   |
| `x /= e` | `x = x / e`   |
| `x %= e` | `x = x % e`   |
| `x++`    | `x = x + 1`   |
| `x--`    | `x = x - 1`   |

Example (decoded):

```go
func main() {
    total := 0
    i := 1
    while i <= 10 { total += i i++ }   // 1 + 2 + ... + 10
    println(total)                     // 55

    xs := []int{10, 20}
    xs[0] += 5                          // slice element as lvalue
    xs[1] *= 3
    println(xs[0], xs[1])              // 15 60
}
```

See `examples/complex/counter.mfl` for a runnable program.

> Note: because `x op= e` desugars to `x = x op e`, an indexed target's
> index expression is evaluated twice (`xs[k] += e` becomes
> `xs[k] = xs[k] + e`). Keep index expressions side-effect free.
