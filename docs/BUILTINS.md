# MFL builtin functions

MFL ships a small set of builtins recognised directly by the type checker
(`types.go`) and lowered to C in `codegen.go`. They are not redefinable. This
page is the authoritative list of their signatures and behaviour.

Notation: `T` is any type, `[]T` a slice of `T`, `num` is "int or float"
(unified at compile time). A return type of `void` means the call produces no
value and may only appear as a statement.

## I/O

| call | signature | notes |
|------|-----------|-------|
| `print(a, b, …)`   | `(…) -> void` | writes its arguments separated by a single space; **no** trailing newline |
| `println(a, b, …)` | `(…) -> void` | like `print` but adds a trailing newline |

`print` and `println` are **statement-only** — using their (void) result in an
expression is a compile-time error.

## Strings and slices

| call | signature | notes |
|------|-----------|-------|
| `len(s)`            | `(string) -> int` or `([]T) -> int` | byte length of a string, or element count of a slice; the kind is resolved by type inference |
| `append(xs, x)`     | `([]T, T) -> []T`  | returns `xs` with `x` appended; the element type unifies `x` with `xs`'s element type |
| `str(x)`            | `(num) -> string`  | decimal text for an int or float |
| `int(x)`            | `(num) -> int`     | truncates a number to an int |

## Concurrency

| construct | form | notes |
|-----------|------|-------|
| `go f(…)`   | statement | spawns `f` as a concurrent worker (goroutine) |
| `sleep(ms)` | `(int) -> void` | blocks the current worker for `ms` milliseconds |

`go` is a keyword, not a function; `sleep` is the simplest way to let spawned
workers make progress before `main` returns. See `examples/complex/goroutines.mfl`.

## Networking

These mirror the POSIX socket calls and back the concurrent HTTP server
example (`examples/complex/http_server.mfl`). File descriptors are plain `int`s.

| call | signature | notes |
|------|-----------|-------|
| `listen(port)`  | `(int) -> int`           | opens a listening TCP socket on `port`, returns its fd |
| `accept(fd)`    | `(int) -> int`           | waits for and returns a connection fd |
| `read(fd)`      | `(int) -> string`        | reads available bytes from `fd` as a string |
| `write(fd, s)`  | `(int, string) -> int`   | writes `s` to `fd`, returns the byte count |
| `close(fd)`     | `(int) -> void`          | closes the fd |

Typical pattern:

```go
func main() {
    s := listen(48080)
    for true {
        conn := accept(s)
        go handle(conn)
    }
}
```
