# MFL tutorial

A getting-started walkthrough for the Machine-First Language. MFL programs are
parsed, type-inferred, and compiled to native code through C, so they run at
C-like speed.

## 1. The encoding model

An `.mfl` file is **base64** (standard encoding), with **one function per line**
and a **blank line between functions**. The encoded form is the source of
truth — there is no separate human-readable file. To turn loose, Go-like text
into an `.mfl`, use `encode`:

```sh
machin encode myprogram.txt > myprogram.mfl
```

`encode` normalizes the readable source (collapsing whitespace), base64-encodes
each function, and joins them one-per-line.

## 2. Build and run

```sh
machin run   myprogram.mfl          # compile to native + execute
machin build myprogram.mfl -o prog  # compile to a native binary
machin build myprogram.mfl --emit-c # print the generated C and stop
```

`--emit-c` is the quickest way to see exactly what MFL lowered your program to.

## 3. Your first function

```go
func main() { println("hello, machine") }
```

Encode it, then run:

```sh
machin encode hello.txt > hello.mfl
machin run hello.mfl
```

Functions take untyped parameters; the compiler infers concrete types from how
each value is used. `:=` declares a new variable, `=` reassigns. Statements are
separated by whitespace — no semicolons needed.

```go
func add(a, b) { return a + b }

func main() { println(add(2, 3)) }   // 5
```

## 4. Type inference

You never write types on locals or parameters. Inference unifies them from use:

```go
func main() {
    k := 0                  // starts int...
    println(2.0 * k)        // ...unified to float by the float literal
}
```

If two uses conflict (e.g. assigning a string to an int variable) the compiler
reports a type error at compile time rather than producing bad code.

## 5. Control flow, loops, and slices

```go
func main() {
    xs := []int{5, 2, 9}
    xs = append(xs, 1)
    i := 0
    for i < len(xs) {        // while-form loop
        println(xs[i])
        i = i + 1
    }
}
```

`if`/`else`, comparison operators (`< <= > >= == !=`), logical operators
(`&& || !`), and modulo (`%`) all behave as in Go. Functions may return slices,
which lets you grow data structures across calls (see `look_and_say` below).

## 6. The example set

These six programs (under `examples/complex/`) exercise the features above. See
[EXAMPLES.md](EXAMPLES.md) for the full catalog.

| Example | What it does | Output |
|---------|--------------|--------|
| `insertion_sort.mfl` | insertion-sort an `[]int`, print it | `1 2 5 5 6 9` |
| `reverse_number.mfl` | reverse a number's digits | `54321 1 8` |
| `sum_digits.mfl` | sum the decimal digits | `15 18 0` |
| `happy_number.mfl` | detect happy numbers (1 = happy) | `1 1 0` |
| `look_and_say.mfl` | first 5 look-and-say terms | `1` / `11` / `21` / `1211` / `111221` |
| `triangular_number.mfl` | nth triangular number | `1 15 55 5050` |

Run any of them:

```sh
machin run examples/complex/look_and_say.mfl
```

## 7. Going further

- Goroutines: `go f()` plus `sleep(ms)` for simple concurrency.
- Networking builtins: `listen`, `accept`, `read`, `write`, `close`.
- `./examples/run.sh` compiles and runs every example (servers auto-skipped).
