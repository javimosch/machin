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

## Maps

A hash map from comparable keys (`int` or `string`) to any value type:

```go
freq := make(map[string]int)
freq["go"] = freq["go"] + 1   // a missing key reads as the zero value (0, "", …)
n := freq["go"]               // index read
println(len(freq))            // number of entries
if has(freq, "go") { ... }    // membership test
delete(freq, "go")            // remove a key

ks := keys(freq)              // iterate via keys(m) -> []K
i := 0
for i < len(ks) {
    println(ks[i], freq[ks[i]])
    i = i + 1
}
```

- `make(map[K]V)` constructs a map. Keys are `int` or `string`; values are any type.
- `m[k]` reads (returning the value type's zero value if absent) and `m[k] = v` writes.
- `len(m)`, `has(m, k)`, `delete(m, k)`, and `keys(m)` (a `[]K` slice) round out the API.
- Iteration order is unspecified.

---

## Structs

A struct type is its own top-level declaration — on disk, its own base64 line:

```go
type User struct {
    name   string
    age    int
    active bool
}
```

Construct with keyed or positional literals, read and assign fields with `.`,
and use them like any other value (passed by value, stored in slices):

```go
u := User{name: "Ada", age: 36, active: true}   // keyed
v := User{"Linus", 54, false}                    // positional
u.age = u.age + 1                                // field assignment
println(u.name, u.age)                           // field access

users := []User{}                                // slice of structs
users = append(users, u)
first := users[0]                                // value copy
```

- Field types are explicit in the declaration (`name string`); everything else
  stays inferred.
- Structs have **value semantics** — assigning or passing one copies it.
- A field can be a scalar, `string`, another struct, or a slice.

---

## Builtins

| Builtin                     | Purpose                                      |
|-----------------------------|----------------------------------------------|
| `print(...)`                | print arguments without a trailing newline   |
| `println(...)`              | print arguments followed by a newline        |
| `len(x)`                    | length of a slice or string                  |
| `append(xs, v)`             | return `xs` with `v` appended                |
| `has(m, k)`                 | whether map `m` contains key `k`             |
| `delete(m, k)`              | remove key `k` from map `m`                  |
| `keys(m)`                   | a slice of map `m`'s keys                    |
| `json(x)`                   | serialize any value to a JSON string         |
| `parse(s, T{})`             | parse a JSON string into a value of `T`'s type |
| `http_body(req)`            | the body of an HTTP message (after the blank line) |
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

### JSON

`json(x)` serializes any value — scalar, `string` (escaped), slice, struct, or
map — to a JSON string. Combined with the networking builtins, this is how an
MFL server returns JSON:

```go
type Todo struct { id int  title string  done bool }

func main() {
    list := []Todo{}
    list = append(list, Todo{id: 1, title: "ship", done: false})
    println(json(list))   // [{"id":1,"title":"ship","done":false}]
}
```

Maps serialize as JSON objects (int keys are stringified). See
`examples/complex/json.mfl` and the JSON API server `examples/complex/json_api.mfl`.

`parse(s, witness)` goes the other way: it parses a JSON string into a value of
the **witness's type**. The witness is only used for its type — pass a zero
value like `Todo{}`, `[]int{}`, or `make(map[string]int)`:

```go
t := parse(s, Todo{})                 // JSON object  -> struct
xs := parse("[1,2,3]", []int{})       // JSON array   -> []int
m := parse(s, make(map[string]int))   // JSON object  -> map
n := parse("42", 0)                   // JSON number  -> int
```

Struct parsing tolerates field reordering, ignores unknown fields, and
zero-fills missing ones. `http_body(req)` returns the body of an HTTP message
(the bytes after the blank line) — so a server can `parse(http_body(req), T{})`.
See `examples/complex/json_parse.mfl` and the echo server
`examples/complex/json_echo_api.mfl`.

### Channels

Channels let goroutines communicate and synchronize without `sleep`:

```go
ch := make(chan int)   // a channel carrying int
go produce(ch)         // a goroutine that sends on ch
v := <-ch              // receive (blocks until a value is available)

func produce(c) {
    c <- 42            // send
}
```

- `make(chan T)` creates a channel of element type `T` (scalar, string, struct, …).
- `ch <- v` sends; `<-ch` receives. A receive blocks until a value arrives.
- The element type is inferred from sends/receives, so `make(chan int)` and a
  later `c <- "x"` would be a compile error.

See `examples/complex/channels.mfl` for a fan-in worker pool.

---

## See also

- [`../README.md`](../README.md) — project overview and the toolchain
- [`../examples/`](../examples/) — runnable programs (`machin run <file>.mfl`)
- `machin build <file>.mfl --emit-c` — inspect the C the compiler emits
