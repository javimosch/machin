# MFL Cookbook

Task-oriented recipes for expressing common patterns in MFL. Every snippet
below is the *decoded* form of the source — remember that on disk a `.mfl`
program is base64, **one function per line, with a blank line between
functions** (see `loadMFL` in `main.go`). Run any example with:

```sh
machin run examples/complex/<name>.mfl
```

## Declare vs. assign

Use `:=` to declare a new variable and `=` to update an existing one:

```go
func main() {
    x := 10      // declare
    x = x + 5    // assign
    println(x)   // 15
}
```

## Iterate with `while`

```go
func main() {
    i := 0
    while i < 5 {
        println(i)
        i = i + 1
    }
}
```

## Recurse

```go
func fact(n) {
    if n <= 1 { return 1 }
    return n * fact(n - 1)
}
```

## Integer division and modulo

`/` is integer division and `%` is the remainder. Together they let you walk
the base-10 digits of a number — the basis of the `digit_sum`,
`reverse_digits`, and `to_binary` examples:

```go
func digit_sum(n) {
    s := 0
    while n > 0 {
        s = s + n % 10
        n = n / 10
    }
    return s
}
```

## Closed-form arithmetic

Not everything needs a loop. Triangular numbers, for instance:

```go
func triangular(n) { return n * (n + 1) / 2 }
```

## Print multiple values

`println` takes any number of arguments and separates them with spaces:

```go
func main() {
    println("fib", 7, "=", 13)   // fib 7 = 13
}
```

## Where to look next

- `examples/complex/` — runnable programs covering loops, recursion,
  arithmetic, slices, and goroutines.
- `examples/complex/goroutines.mfl` and `http_server.mfl` — concurrency.
- The example tests (`*_test.go`) show how each program is compiled to native
  code and exercised end-to-end.
