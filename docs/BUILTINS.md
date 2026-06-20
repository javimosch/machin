# MFL Built-in Functions

MFL ships a small set of built-in functions that are always in scope — no
import is needed. Types are inferred, so the signatures below use MFL's
inferred kinds (`int`, `float`, `bool`, `string`, `[]T`) for documentation only;
you never write them in source.

> Remember: `.mfl` source is base64, one function per line. The snippets below
> show the **decoded** form for readability. Run any example with
> `machin run <file.mfl>`.

## I/O

### `print(args...)`
Writes each argument to stdout separated by a single space, **without** a
trailing newline. Accepts any number of arguments of any printable type
(`int`, `float`, `bool`, `string`).

```go
func main() { print("x =", 42) print(" done") }
// stdout: x = 42 done
```

### `println(args...)`
Like `print`, but appends a trailing newline. With no arguments it prints a
blank line.

```go
func main() { println("hello", 1 == 1) }
// stdout: hello true
```

## Strings & conversion

### `str(n) -> string`
Converts a numeric value (`int` or `float`) to its string representation.
Integers print exactly; floats use a compact `%g` format.

```go
func main() { println("n" + str(7)) }
// stdout: n7
```

### `int(n) -> int`
Truncates a numeric value to an `int` (e.g. `float` → `int`).

```go
func main() { println(int(3.9)) }
// stdout: 3
```

## Sequences

### `len(x) -> int`
Returns the length of a `string` (in bytes) or a slice `[]T`.

```go
func main() { println(len("abc"), len([]int{4, 5})) }
// stdout: 3 2
```

### `append(s, elem) -> []T`
Returns the slice `s` with `elem` appended. The element type must match the
slice's element type. As in Go, assign the result back: `s = append(s, x)`.

```go
func main() { xs := []int{} xs = append(xs, 1) xs = append(xs, 2) println(len(xs)) }
// stdout: 2
```

## Concurrency

### `sleep(ms)`
Suspends the current goroutine for `ms` milliseconds.

```go
func main() { sleep(10) println("awake") }
```

Goroutines are launched with the `go` statement; see
[`examples/complex/goroutines.mfl`](../examples/complex/goroutines.mfl).

## Networking

These wrap the POSIX socket calls and underpin the concurrent HTTP server
example ([`examples/complex/http_server.mfl`](../examples/complex/http_server.mfl)).

### `listen(port) -> int`
Opens a TCP listening socket bound to `port` and returns its file descriptor.

### `accept(fd) -> int`
Blocks until a client connects to the listening socket `fd`, returning a new
connection file descriptor.

### `read(fd) -> string`
Reads available bytes from connection `fd` and returns them as a string.

### `write(fd, s) -> int`
Writes string `s` to connection `fd`, returning the number of bytes written.

### `close(fd)`
Closes the file descriptor `fd`.

```go
func main() {
  fd := listen(8080)
  c := accept(fd)
  _ := read(c)
  write(c, "HTTP/1.1 200 OK\r\n\r\nhi")
  close(c)
}
```

---

For language syntax (functions, loops, operators), see the example programs
under [`examples/complex/`](../examples/complex/).
