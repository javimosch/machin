# MFL Language Reference

MFL (Machine-First Language) is a statically-typed, Go-flavored language shaped
for machine authoring: minimal syntax, no type annotations, one canonical
function per line. Its on-disk form is plain canonical text. It compiles to
native code through C (`cc -O2`).

This document describes the surface syntax — which is exactly what a `.mfl` file
contains, one normalized declaration per line.

> For the complete, version-exact surface (every keyword and builtin with
> signatures, idioms, gotchas), run `machin guide` (`--text` for prose) — it is
> generated from the compiler's own catalog and cannot drift. This tour is a
> readable companion.

---

## Source form

A `.mfl` file is a sequence of declarations:

- **One function (or type) per line.** Each non-blank line is one `func ...` or
  `type ...`, normalized to a single line.
- **A blank line separates declarations.**
- The source is plain canonical text — greppable and diffable. You author intent
  and let agents write the code; nothing is hidden behind an encoding.

```bash
# It is just text — read or grep it directly:
cat examples/demo.mfl
grep -l fizzbuzz examples/*.mfl
```

A dense base64 "packed" form is available for distribution via `machin pack`;
`machin run` reads either form.

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
| `bytes`   | `bytes("hi")`, `from_hex("ff00")` | NUL-safe binary buffer; `len(b)` counts raw bytes; `println(b)` prints hex; strings truncate at NUL, bytes do not |

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
- The last parameter may be **variadic** (`name...`): it collects the trailing
  arguments into a slice. Call it with extra args (`sum(1, 2, 3)`) or spread a
  slice (`sum(xs...)`). See `examples/complex/variadic.mfl`.
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

## Generics

There is no generic syntax — functions are **implicitly generic**. Because types
are inferred, a function places no constraint on its parameters beyond how it
uses them, so the same source function works at many types:

```go
func id(x) { return x }
func third(xs) { return xs[2] }

func main() {
    println(id(42), id("hello"), id(3.14))          // int, string, float
    println(third([]int{1, 2, 3}), third([]string{"a", "b", "c"}))
}
```

Each call is compiled by **monomorphization**: the compiler stamps out one
specialized C function per concrete call-site signature (deduplicated), so there
is no boxing and no runtime cost — `id` above becomes three native functions.
Recursion is monomorphic (a function is one concrete type within a single
instantiation).

---

## Functions as values (closures)

Functions are values. A `func(params) { ... }` literal is an expression you can
store, pass, return, and call. A literal **captures** variables from the
enclosing scope (by reference — mutable captured state, Go semantics):

```go
func adder(n) {
    return func(x) { return x + n }   // captures n
}

func map_slice(xs, f) {               // higher-order function
    out := []int{}
    for _, v := range xs {
        out = append(out, f(v))
    }
    return out
}

func main() {
    inc := adder(1)
    println(inc(5))                                   // 6
    doubled := map_slice([]int{1, 2, 3}, func(x) { return x * 2 })
    sum := func(a, b) { return a + b }(2, 3)          // call a literal directly
}
```

- Function values are compiled by closure conversion (lambda-lifting): each
  literal becomes a top-level function plus a captured environment.
- Capture is **by reference** — a closure shares the captured variables with
  the enclosing scope; later changes to them (by either side) are visible to
  the closure. See `examples/complex/counter.mfl` for a closure that mutates
  a captured counter across calls.
- A function value holds a single return value (or none).

---

## Multiple return values

A function may return more than one value, and a call destructures across names:

```go
func divmod(a, b) { return a / b, a % b }

func lookup(m, k) { return m[k], has(m, k) }   // Go-style (value, ok)

func main() {
    q, r := divmod(17, 5)        // 3, 2
    v, ok := lookup(m, "x")      // comma-ok
    _, rem := divmod(20, 6)      // _ ignores a value
    a, b = b, a                  // parallel assignment (swap)
}
```

- The number of returned values is fixed per function (from its `return`s).
- A multi-value call may only appear as the sole right-hand side of a
  multi-assignment — not nested in another expression.
- `a, b = e1, e2` evaluates both right-hand sides before assigning. Use `_` to
  discard a value.

Return values may also be **named** in the signature. Named returns are
zero-initialized locals; a bare `return` (or falling off the end) yields them:

```go
func divmod(a, b) (q, r) {
    q = a / b
    r = a % b
    return
}
```

See `examples/complex/multi_return.mfl` and `examples/complex/named_returns.mfl`.

---

## Control flow

```go
if n % 15 == 0 { println("FizzBuzz") } else if n % 3 == 0 { println("Fizz") } else { println(n) }

while i <= n { acc = acc * i  i = i + 1 }

for i < len(xs) { total = total + xs[i]  i = i + 1 }

for i, v := range xs { total = total + v }       // index + value
for _, v := range xs { total = total + v }       // value only
for k, v := range m  { ... }                     // map key + value
for i, c := range s  { ... }                     // string index + character

for { if done { break }  step() }                // bare infinite loop
for i < n { if skip(i) { continue }  use(i)  i = i + 1 }
```

- `if` / `else if` / `else` — conditions are `bool` expressions.
- `while cond { ... }` — loops while `cond` holds.
- `for cond { ... }` — equivalent condition-only loop (Go-style `for`).
- `for { ... }` — bare `for` with no condition loops forever; exit with
  `break`.
- `for k, v := range x { ... }` — iterate a slice (index, element), map (key,
  value), or string (index, 1-char). The first variable is the index/key; the
  second (optional) is the value. Use `_` to ignore either. Map iteration order
  is unspecified.
- `break` — exits the innermost `while`/`for` loop immediately.
- `continue` — skips to the next iteration of the innermost `while`/`for` loop.

---

## Operators

| Category    | Operators                          |
|-------------|------------------------------------|
| Arithmetic  | `+`  `-`  `*`  `/`  `%`             |
| Comparison  | `==`  `!=`  `<`  `<=`  `>`  `>=`    |
| Logical     | `&&`  `\|\|`  `!` (short-circuit; `!` is unary) |
| String      | `+` (concatenation)                |

`%` is integer-only. `/` on `int` is integer division. Comparisons and logical
operators yield `bool`.

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

## Strings

Strings concatenate with `+` and have a `len`. A set of builtins covers the
common text operations — enough to parse an HTTP request line and route on it:

```go
line := "GET /users/42 HTTP/1.1"
f := split(line, " ")          // ["GET", "/users/42", "HTTP/1.1"]
method := f[0]                 // "GET"
seg := split(f[1], "/")        // ["", "users", "42"]
if has_prefix(f[1], "/users/") {
    id := substr(f[1], 7, len(f[1]))
}
```

See `examples/complex/strings.mfl` and the HTTP router `examples/complex/router_api.mfl`.

---

## Structs

A struct type is its own top-level declaration — on disk, its own line:

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
| `input()`                   | read one line from stdin; newline stripped, `""` at EOF |
| `len(x)`                    | length of a slice or string                  |
| `append(xs, v)`             | return `xs` with `v` appended                |
| `has(m, k)`                 | whether map `m` contains key `k`             |
| `delete(m, k)`              | remove key `k` from map `m`                  |
| `keys(m)`                   | a slice of map `m`'s keys                    |
| `json(x)`                   | serialize any value to a JSON string         |
| `parse(s, T{})`             | parse a JSON string into a value of `T`'s type |
| `json_get(json, path)`      | value at a jq-style path → `(value, err)`; `err` is `""`/`"notfound"`/`"path"`/`"parse"` — **multi-assign only** |
| `http_body(req)`            | the body of an HTTP message (after the blank line) |
| `substr(s, i, j)`           | the substring `s[i:j]` (bounds-clamped)      |
| `index(s, sub)`             | first index of `sub` in `s`, or `-1`         |
| `contains/has_prefix/has_suffix(s, x)` | substring / prefix / suffix tests |
| `charat(s, i)`              | the 1-character string at index `i`          |
| `to_upper(s)` / `to_lower(s)` | case conversion                            |
| `trim(s)`                   | strip leading/trailing whitespace            |
| `replace(s, old, new)`      | replace all occurrences                      |
| `split(s, sep)`             | split into a `[]string`                      |
| `join(xs, sep)`             | join a `[]string` with `sep`                 |
| `str(n)`                    | convert an `int` or `float` to its `string`  |
| `int(n)`                    | convert a numeric value to `int` (truncates) |
| `url_encode(s)`             | percent-encode a string for URLs (RFC 3986: keeps `A-Za-z0-9-._~`, encodes everything else, space → `%20`) |
| `url_decode(s)`             | percent-decode a URL component (lenient: `+` → space, malformed `%XX` passes through unchanged) |
| `base64_encode(s)`          | base64-encode a string → standard padded output (`A-Za-z0-9+/=`) |
| `base64_decode(s)`          | base64-decode a string → string; **lenient**: accepts standard *and* URL-safe alphabets, ignores missing padding |
| `base64_encode_bytes(b)`    | base64-encode raw `bytes` → string (binary-safe; use instead of `base64_encode` for non-text payloads) |
| `base64_decode_bytes(s)`    | base64-decode → raw `bytes` (lenient; binary-safe; e.g. SCRAM salt or binary token) |
| `sha256(s)`                 | SHA-256 of string `s` → lowercase hex string (byte-exact against `sha256sum`) |
| `hmac_sha256(key, msg)`     | HMAC-SHA256(key, msg) → lowercase hex string (RFC 2104; use for webhook signature verification) |
| `sleep(ms)`                 | suspend the current goroutine (milliseconds) |
| `listen(port)`              | open a TCP listening socket                  |
| `accept(fd)`                | accept a connection, return its socket fd    |
| `read(fd)` / `write(fd, s)` | read from / write to a socket — **one `read(2)` of up to 65535 bytes, not a whole message** (see note below) |
| `close(fd)`                 | close a socket                               |
| `https_get(url)`            | GET over TLS (or plain http://) → body string (`""` on error) |
| `https_post(url, body)`     | POST with string body over TLS (or plain http://) → body string |
| `http_get(url)`             | GET → `(status int, body string, err string)` — **multi-assign only** |
| `http_request(method, url, headers, body)` | authenticated HTTP(S): headers are `[]string` of `"Key: Value"` lines; caller owns `Content-Type` → `(status int, body string, err string)` — **multi-assign only** |
| `bytes(s)`                  | make a `bytes` value from a string's raw bytes |
| `bytes_str(b)`              | `bytes` → `string` (NUL-terminated; truncates at embedded `0`) |
| `to_hex(b)`                 | lowercase hex of a `bytes` value             |
| `from_hex(s)`               | parse hex string → `bytes` (skips non-hex chars) |
| `byte_at(b, i)`             | byte value 0–255 at index `i` (−1 if out of range) |
| `bytes_sub(b, start, end)`  | sub-range `[start, end)` of a `bytes` value  |
| `bytes_concat(a, b)`        | concatenate two `bytes` values               |

### Raw sockets (`listen` / `accept` / `read` / `write`)

**`read(fd)` is one `read(2)` syscall, returning whatever is currently in the
socket's buffer (up to 65535 bytes) — not a whole message.** TCP is a byte
stream: a request larger than ~64KB, or one whose bytes simply haven't all
arrived yet (common under load, or for any non-trivial POST body), is
silently truncated. A single `read(conn)` is only safe when you know the
peer sends one small, complete message per read (e.g. a line-based
protocol) — never assume it returns a full HTTP request.

To read a complete HTTP request, loop `read_bytes(fd)` (the NUL-safe,
`bytes`-returning sibling of `read`) until you've seen the `\r\n\r\n`
header/body separator, then keep looping until you have `Content-Length`
bytes of body — this is exactly what `framework/machweb.src`'s
`read_request_bytes` does; read that function before writing your own raw
socket server. See also [issue #91](https://github.com/javimosch/machin/issues/91).

### SQLite

These builtins require `libsqlite3`; the linker flag (`-lsqlite3`) is added
automatically when any `sqlite_*` builtin is used (the library must be installed
on the build/runtime host, e.g. `apt install libsqlite3-dev` on Debian/Ubuntu).

| Builtin                          | Purpose                                                                 |
|----------------------------------|-------------------------------------------------------------------------|
| `sqlite_open(path) -> int`       | open or create a SQLite database file → handle (`0` on failure); `":memory:"` opens a transient in-memory db |
| `sqlite_exec(h, sql[, []string]) -> int` | run result-less SQL (CREATE/INSERT/UPDATE/DELETE); optional `[]string` binds `?` params (injection-safe); returns `0` on success |
| `sqlite_query(h, sql[, []string]) -> string` | run a SELECT → **JSON array-of-row-objects** string; optional `[]string` binds `?` params; decode with `parse(rows, []T{})` for a typed slice, or `json_get` for a single field |
| `sqlite_close(h) -> int`         | close the database handle                                               |

See `examples/complex/sqlite_crud.mfl` for a working CRUD example using an
in-memory database.

### Crypto (over `bytes`)

These builtins require OpenSSL libcrypto; the linker flag is added automatically
when any crypto builtin is used.

> **Safety rules:**
> - `aes_gcm_encrypt` returns `ct||tag` (ciphertext concatenated with a 16-byte
>   auth tag); `aes_gcm_decrypt` expects that exact layout.
> - `aes_gcm_decrypt` and `aes_cbc_decrypt` return **empty `bytes` on
>   authentication / padding failure** — not an error.  Always check
>   `len(result) > 0` before using the plaintext.
> - AES-GCM IVs must be **12 bytes** and must **never be reused** with the same
>   key (use `rand_bytes(12)` per encryption).
> - X25519 private keys and Ed25519 seeds are **32 bytes**.

| Builtin                                        | Purpose                                                         |
|------------------------------------------------|-----------------------------------------------------------------|
| `rand_bytes(n)`                                | `n` cryptographically-random bytes (CSPRNG)                     |
| `sha256_bytes(b)`                              | SHA-256 of `b` → 32-byte digest (binary-safe)                   |
| `sha1_bytes(b)`                                | SHA-1 of `b` → 20-byte digest (legacy auth only)                |
| `hmac_sha256_bytes(key, msg)`                  | HMAC-SHA256(key, msg) → 32 bytes                                |
| `hkdf_sha256(ikm, salt, info, length)`         | HKDF-SHA256 key derivation → `length` bytes                     |
| `pbkdf2_sha256(pass, salt, iters, dklen)`      | PBKDF2-HMAC-SHA256 → derived key of `dklen` bytes (password hashing) |
| `x25519_pub(priv32)`                           | X25519 public key from a 32-byte private key                    |
| `x25519_shared(priv32, pub32)`                 | X25519 ECDH shared secret → 32 bytes                            |
| `ed25519_pub(seed32)`                          | Ed25519 public key from a 32-byte seed                          |
| `ed25519_sign(seed32, msg)`                    | Ed25519 sign → 64-byte signature                                |
| `ed25519_verify(pub32, msg, sig64)`            | Ed25519 verify → `bool`                                         |
| `aes_gcm_encrypt(key, iv12, pt, aad)`          | AES-GCM encrypt → `ct\|\|tag` (key 16 or 32 bytes; iv 12 bytes; **never reuse iv+key pair**) |
| `aes_gcm_decrypt(key, iv12, ct_tag, aad)`      | AES-GCM decrypt → plaintext (**empty `bytes` on auth failure**) |
| `aes_cbc_encrypt(key, iv, pt)`                 | AES-CBC encrypt, PKCS#7 padded (key 16 or 32 bytes)             |
| `aes_cbc_decrypt(key, iv, ct)`                 | AES-CBC decrypt → plaintext (**empty `bytes` on bad padding**)  |

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

`json_get(json, path)` digs a single value out of a JSON string by a
jq-style path (`.field`, `[index]`, chained) without parsing the whole
document into a struct — handy when you only need one field, or the shape
isn't known ahead of time. It's **multi-assign only**, returning
`(value, err)`: `value` is the raw JSON text at that path, and `err` is
`""` on success, `"notfound"` if the path doesn't resolve, `"path"` if the
path string itself is malformed, or `"parse"` if the JSON at that location
can't be read:

```go
s := json(Todo{id: 1, title: "ship it", done: false})
title, err := json_get(s, ".title")   // title == `"ship it"`, err == ""
_, err = json_get(s, ".nope")         // err == "notfound"
```

See `examples/complex/json_get.mfl`, and `sqlite_query`'s use of it for
single-field access on query rows.

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

### HTTP client

`https_get` and `https_post` return the response body as a string (empty on
error) and are suitable for quick one-liners.  `http_get` and `http_request`
return `(status, body, err)` — they are **multi-assign only** (using them as a
single value is a compile error) and expose the status code and a descriptive
error string:

```go
// Simple GET — branches on err
status, body, err := http_get("https://example.com/")
if len(err) > 0 { println("error: " + err)  exit(1) }
println(str(status) + " " + str(len(body)) + " bytes")

// Authenticated request — headers are "Key: Value" strings; caller owns Content-Type
status2, body2, err2 := http_request(
    "GET",
    "https://api.example.com/data",
    []string{"Authorization: Bearer my-token", "Accept: application/json"},
    "")
```

See `examples/complex/http_client_api.mfl` for the full runnable example.

---

## Memory / arenas

Each goroutine allocates from its own per-goroutine arena, which is garbage
collected automatically — there is no manual `free`. Within a goroutine, a
scoped `arena { }` block bump-allocates everything created inside it and
reclaims that memory as soon as the block exits, which avoids GC pressure in
hot loops:

```go
func render(i) {
    row := "row-" + str(i)
    return len(row)
}

func main() {
    checksum := 0
    n := 0
    while n < 100000 {
        arena {
            checksum = checksum + render(n)
        }
        n = n + 1
    }
    println("checksum:", checksum)
}
```

Values that need to outlive the block (like `checksum` above) must be
assigned to a variable declared outside it. See
`examples/complex/arena.mfl` for the full runnable example.

---

## Safety checks (`--safe`)

Passing `--safe` to `machin build` or `machin run` inserts runtime checks for:

- **bounds** — slice/array/string indexing out of range
- **div-zero** — integer division or modulo by zero
- **overflow** — integer arithmetic overflow

```
machin build app.mfl --safe
machin run   app.mfl --safe
```

These checks add overhead, so they are opt-in rather than always on; a
separate `--race-safe` flag (not related to `--safe`) refuses to build if a
data race is inferred in `go`/channel usage.

---

## C FFI (extern)

MFL can call C functions directly. An `extern` block declares the library, its
header, link flags, and the functions (or C structs) to expose:

```go
extern "m" { header "math.h" link "m" fn sqrt(float) float fn pow(float, float) float }

func main() {
    println("sqrt(2) =", sqrt(2.0))
    println("2^10  =", pow(2.0, 10.0))
}
```

- `"m"` — an informational name for the library.
- `header "h.h"` — emits `#include <h.h>` so the real C prototype is in scope.
  Omit it and machin synthesizes a prototype from the declared signature.
- `link "l"` — passes `-l<l>` to the compiler. Repeat for multiple libraries
  (`link "raylib" link "GL" link "m"`); they are emitted in order.
- `cflags "..."` — passes extra flags (`-I`/`-L` paths, etc.) to the C compiler.
- `fn Name(t, …) ret` — a foreign function. A missing return type means `void`.

See `examples/complex/ffi_math.mfl` for the runnable version.

### FFI scalar types

| MFL type | C type        | Notes                                       |
|----------|---------------|---------------------------------------------|
| `int`    | `int64_t`     |                                             |
| `i32`    | `int32_t`     | use for 32-bit C params (e.g. raylib)       |
| `i16`    | `int16_t`     |                                             |
| `i8`     | `int8_t`      |                                             |
| `u64`…`u8` | `uint64_t`…`uint8_t` |                                  |
| `float` / `f64` | `double` |                                        |
| `f32`    | `float`       |                                             |
| `bool`   | `int`         |                                             |
| `string` | `const char*` |                                             |
| `ptr`    | `void*`       | opaque handle, held as `int` in MFL (see below) |

### `cstruct` — by-value C structs

`cstruct Name { field ctype … }` declares a C struct. machin synthesizes a
matching MFL struct and marshals it at the boundary:

```go
extern "c" {
    header "stdlib.h"
    cstruct div_t { quot i32  rem i32 }
    fn div(i32, i32) div_t
}

func main() {
    r := div(17, 5)
    println("17 / 5 =", r.quot, "remainder", r.rem)
}
```

A field may be another `cstruct` (declare the inner one first), enabling nested
by-value aggregates like raylib's `Camera3D`.

See `examples/complex/ffi_struct.mfl`.

### Opaque handles (`ptr` and opaque `cstruct`)

Use **`ptr`** for a single opaque C pointer (held as an `int`):

```go
extern "c" {
    header "stdio.h"
    fn fopen(string, string) ptr
    fn fputs(string, ptr) i32
    fn fclose(ptr) i32
}

func main() {
    f := fopen("/tmp/out.txt", "w")
    if f == 0 { println("error") } else {
        fputs("hello from MFL\n", f)
        fclose(f)
    }
}
```

Use **`cstruct Name {}`** (empty body) for a by-value C type whose fields you
don't need in MFL (e.g. raylib's `Sound`/`Music`/`Font`). machin holds the full
C struct and passes it back to functions — you can store it in a variable or
slice, but cannot construct or field-access it.

See `examples/complex/ffi_ptr.mfl`.

### Multi-`link` and `cflags`

```go
extern "raylib" {
    cflags "-I/usr/local/include"
    link "raylib"  link "GL"  link "m"
    header "raylib.h"
    fn InitWindow(i32, i32, string)
    fn WindowShouldClose() bool
    fn CloseWindow()
}
```

Libraries are linked in declaration order; `cflags` entries are passed directly
to the C compiler. See `examples/gui/` for a working raylib desktop application.

---

## See also

- `machin guide` (`--text` for prose) — the complete, version-exact catalog of
  every keyword and builtin, generated from the compiler's own source of truth
- [`../README.md`](../README.md) — project overview and the toolchain
- [`../examples/`](../examples/) — runnable programs (`machin run <file>.mfl`)
- `machin build <file>.mfl --emit-c` — inspect the C the compiler emits
