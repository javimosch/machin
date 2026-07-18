# The MFL Language Specification

Version 0.112.0

MFL (Machine-First Language) is a statically-typed, Go-flavored backend language
**shaped for machine authoring**: minimal syntax, no type annotations, one
canonical function per line. Its on-disk form is plain canonical text; it is
compiled to native code through C. This document specifies the language as
implemented by the `machin` toolchain.

> This is a reference specification. For a gentler tour see
> [`docs/LANGUAGE.md`](docs/LANGUAGE.md); for runnable programs see
> [`examples/`](examples/).

---

## 1. Overview

- **Machine-first.** The language is shaped for agents to write and edit
  cheaply: terse, type-inferred, and laid out one normalized declaration per
  line (a blank line between declarations). The source is plain canonical text —
  greppable and diffable — not a form humans would enjoy authoring, but one they
  state intent toward and let agents produce. A dense base64 "packed" form is
  available via `machin pack` for distribution; `machin run` reads either.
- **Statically typed, no annotations.** Every value has a type known at compile
  time, inferred by unification. There is no type syntax except struct field
  types and the element types in `make`/composite literals.
- **Compiled, native.** Programs are translated to C99 and compiled with
  `cc -O2`. Values are unboxed; performance matches hand-written C.

---

## 2. Program structure

A `.mfl` file is a sequence of **declarations**, one normalized declaration per
non-blank line, a blank line between them. The canonical form is *tightened*:
whitespace adjacent to an operator or punctuation token is dropped (it only
remains where the lexer needs it, between two word tokens), which lowers the
token cost of writing and editing without changing meaning:

```
func fib(n){if n<2{return n}return fib(n-1)+fib(n-2)}

func main(){println(fib(10))}
```

Each line is exactly one top-level declaration: a **function** (`func ...`) or a
**struct type** (`type ...`), in the grammar described in the rest of this
document. Whitespace within a declaration is insignificant except as a token
separator, so spaced-out input is accepted and `encode` produces the tight
canonical form. A line that contains no whitespace is treated as a
base64-**packed** declaration and decoded first, so `machin pack` output also
loads. (The grammar examples below are shown spaced for readability; both forms
parse identically.)

A program must define a function named `main` with no parameters and no return
value; it is the entry point.

The toolchain commands:

| Command | Effect |
|---------|--------|
| `machin run <file.mfl>` | compile to native and execute |
| `machin build <file.mfl> [-o out]` | compile to a native binary |
| `machin build <file.mfl> --emit-c` | print the generated C |
| `machin encode <src>` | encode loose declaration text into canonical `.mfl` |
| `machin pack <file.mfl>` | emit the dense base64 form (distribution) |
| `machin guide [--text]` | print the full feature catalog (JSON by default) — keywords, builtins, idioms, gotchas — for agents to load in one call |

`machin encode <src...>` is the authoring path: it accepts one or more `.src`
files of loose, Go-like declaration text (any whitespace, comments allowed),
concatenates them in the order given, splits the combined text into
per-function blocks, strips comments, and normalizes each block to one
canonical line. The result is parsed and type-checked before being printed, so
`encode` fails on a type error instead of emitting a broken `.mfl`. Passing
multiple files lets a shared module (e.g. a framework) precede an
app-specific file — see [`docs/LANGUAGE.md`](docs/LANGUAGE.md#authoring-machin-encode)
and [`framework/`](framework/) for a worked example.

---

## 3. Lexical elements

The decoded text of a declaration is tokenized as follows.

- **Comments.** `// ...` to end of line. (Stripped during encoding; not present
  in the canonical single-line form.)
- **Identifiers.** `[A-Za-z_][A-Za-z0-9_]*`. `_` is the blank identifier.
- **Integer literals.** decimal (`42`), hex (`0xff`), binary (`0b1010`), or octal
  (`0o17`); underscores allowed as digit-group separators in any base
  (`1_000_000`, `0xff_00`). Underscores may only sit between digits — a leading,
  trailing, or doubled `_` is a parse error.
- **Float literals.** digits with a `.`, e.g. `3.14`, `0.5`, or with a decimal
  exponent (`e`/`E`, optionally signed), e.g. `1e3`, `1.5e-9`, `6.022e23` — a
  literal carrying an exponent is a float even without a `.` (`1e3`). Underscores
  are likewise allowed as separators between digits in either the mantissa or the
  exponent (`3_000.5`, `1_000e1_0`).
- **String literals.** `"..."` with escapes `\n \t \r \" \\`. No `\uXXXX`
  escape form — embed non-ASCII as raw UTF-8 bytes in the source file instead
  (a tool generating `.src`/`.mfl` text should emit real UTF-8, not escape it —
  e.g. Python's `json.dumps(..., ensure_ascii=False)`).
- **Keywords.** `func return if else while for range true false nil var go type
  struct chan make map arena extern export break continue select`.
- **Operators and punctuation.** `+ - * / % & | ^ << >> == != < <= > >= && || ! = := <- .
  : , ; ( ) { } [ ]`.

---

## 4. Types

| Type | Description | C representation |
|------|-------------|------------------|
| `int` | 64-bit signed integer | `int64_t` |
| `float` | IEEE-754 double | `double` |
| `bool` | boolean | `int` (0/1) |
| `string` | immutable text; zero value `""` | `char*` |
| `bytes` | NUL-safe binary buffer (pointer + length) | `mfl_bytes` |
| `[]T` | slice of `T` (header + backing array) | `mfl_slice` |
| `map[K]V` | hash map, `K` is `int` or `string` | `mfl_map*` |
| `chan T` | channel of `T` | `mfl_chan*` |
| `func(...) R` | function value / closure | `mfl_closure` |
| named `struct` | record of typed fields | generated `struct` |

Types are **inferred**: a binding's type is determined by how it is used.
Mixing incompatible types is a compile-time error. An integer literal has a
flexible numeric type that resolves to `int` unless unified with a `float`.

### 4.1 Zero values

`int`/`bool` → `0`, `float` → `0.0`, `string` → `""`, slice/map/chan/func → an
empty/null value. A map read of an absent key yields the value type's zero value.

---

## 5. Declarations

### 5.1 Struct types

```go
type User struct {
    name   string
    age    int
    tags   []string
}
```

A field type is `int`, `float`, `bool`, `string`, `bytes`, `[]T`, `map[K]V`,
`func` (a function value), or another struct name. Structs have **value semantics**:
assigning or passing one copies it.

The `func` type denotes a function value whose signature is inferred from use —
it lets closures be stored in slices, maps (`make(map[string]func)`), and struct
fields, which is how a router keeps a table of handlers.

### 5.2 Functions

```go
func add(a, b) { return a + b }
```

Parameters are untyped in the syntax; their types are inferred. A function may
return zero, one, or several values (§9). Functions are **implicitly generic**
(§11).

The last parameter may be **variadic**, written `name...`. It collects the
trailing call arguments into a slice:

```go
func sum(nums...) {            // nums is a slice
    t := 0
    for _, n := range nums { t = t + n }
    return t
}

sum(1, 2, 3)                   // collect: nums = []int{1, 2, 3}
sum(xs...)                     // spread: nums = xs
```

### 5.3 Variables

```go
x := expr      // declare, type inferred from expr
var x = expr   // same (inside a function)
x = expr       // assign (x must exist; types must match)
```

Variables have **function scope** (a name denotes one binding, of one type,
throughout the function body).

A `var name = expr` written at **top level** (not inside a function) is a
**package global**: a single mutable binding shared by every function, its type
inferred from the initializer and uses. Unlike a local it persists for the life
of the program (and, under the wasm target, across exported-function calls — so a
component can own its state). `=` to its name assigns the global; `:=` inside a
function introduces a local that may shadow it. Globals are in scope inside
closures too (referenced directly, not captured). Their initializers run once at
startup (before `main`; at `_initialize` for a wasm reactor).

---

## 6. Expressions

- **Literals:** integer, float, string, `true`, `false`. `nil` is a reserved
  keyword and parses as a literal expression, but it is **not yet implemented**:
  it is rejected with a type-check error wherever it appears, since no value
  type accepts it yet (no slice/map/chan/func-optional support). See §16.
- **Composite literals:** `[]T{...}`, `T{f: v, ...}` / `T{v, ...}`.
- **Construction:** `make(map[K]V)`, `make(chan T)`, `func(params){...}`.
- **Operators**, by increasing precedence:
  `||` · `&&` · `== != < <= > >=` · `+ - | ^` · `* / % << >> &` · unary `- ! ^ <-`.
- **Bitwise** `& | ^ << >>` (and unary `^`, complement) are **`int`-only** —
  Go semantics and precedence (`<< >> &` bind like `* / %`; `| ^` like `+ -`).
- **Arithmetic** `+ - * / %`: numeric operands. `/` of two `int` is integer
  division; `%` is `int`-only. A flexible numeric **literal** promotes to `float`
  on contact (`3.0 + 2`), but a **concrete** `int` — a function return, `byte_at`,
  `len`, a typed parameter, an `int`-slice element — does **not**; mixing it with
  a `float` is an `int vs float` error. Convert it with `float(x)` (and `int(x)`
  the other way). The same applies to `f32`/`f64` fields of FFI `cstruct`s.
- **`+`** on two `string`s concatenates.
- **Comparisons** yield `bool`; `&&`/`||` short-circuit.
- **Indexing** `x[i]`: slice (by `int`) or map (by key); also map/slice
  assignment `x[i] = v`.
- **Field access** `x.f` and assignment `x.f = v`.
- **Calls** `f(args)`; a function value may also be called: `g()(x)`, `fs[i](x)`.
- **Receive** `<-ch`.
- **Evaluation order** is **left-to-right** (as in Go): the operands of a binary
  operator, the arguments of a call, the elements of a slice/struct literal, and
  the values of a multi-return `return` are evaluated in source order, so their
  side effects are sequenced predictably.

---

## 7. Statements

- Expression statement, assignment (`=`, `:=`, `var`).
- Multiple/parallel assignment: `a, b := f()` / `a, b = e1, e2` (§9).
- `if cond { ... } else if cond { ... } else { ... }` — conditions are `bool`.
- Loops:
  - `while cond { ... }`
  - `for cond { ... }` and `for { ... }` (infinite)
  - `for k, v := range x { ... }` over a slice (index, element), map (key,
    value), or string (index, 1-char). Either variable may be `_`.
  - `for v := range ch { ... }` over a channel: receives each value until the
    channel is closed and drained, then ends. One variable (the element).
  - `v, ok := <-ch` (comma-ok receive): `ok` is `false` once the channel is
    closed and drained (`v` is then the zero value). Works standalone and as a
    `select` case (`case v, ok := <-ch:`); a closed channel makes its select case
    ready, firing with `ok == false`.
  - `break` exits the innermost loop; `continue` skips to its next iteration.
- `return`, `return e`, `return e1, e2`.
- `x[i] = v`, `x.f = v`.
- `ch <- v` (send).
- `go f(args)` (§10).
- `select { case v := <-ch: ... case ch <- x: ... default: ... }` — waits on
  multiple channel ops; the first ready case runs (receives are tried before
  sends, in source order), `default` runs if none is ready, and with no default
  and nothing ready it blocks. `break`/`continue` inside a case affect the
  enclosing loop (not the select); use a flag to leave a `for`+`select`.

---

## 8. Builtins

| Builtin | Signature | Purpose |
|---------|-----------|---------|
| `print`, `println` | `(...) -> ` | write arguments (no / trailing newline) |
| `input` | `() -> string` | read one line from stdin (newline stripped; `""` at EOF) |
| `read_stdin` | `() -> string` | read all of stdin verbatim until EOF (exact bytes; no line splitting) |
| `args` | `() -> []string` | command-line arguments (`args()[0]` is the program path) |
| `env` | `(string) -> string` | environment variable (`""` if unset) |
| `now` | `() -> int` | wall-clock time in Unix seconds |
| `now_ms` | `() -> int` | wall-clock time in milliseconds (for latency) |
| `time_fields` | `(int) -> []int` | decompose a Unix timestamp (local time) → `[year, month(1-12), day, hour, minute, second, weekday(0=Sun), yearday]` |
| `time_format` | `(int, string) -> string` | format a Unix timestamp (local time) with a `strftime(3)` pattern (`%Y %m %d %H %M %S %A %B %z %Z %F %T` …) |
| `time_format_utc` | `(int, string) -> string` | like `time_format` but in UTC (`gmtime`) — the form iCalendar / RFC-3339 timestamps want |
| `time_make` | `(int, int, int, int, int, int) -> int` | build a Unix timestamp from local calendar fields `(year, month, day, hour, minute, second)` — inverse of `time_fields`; `mktime(3)` normalizes out-of-range fields |
| `parse_int` | `(string) -> int` | parse an integer (0 if not numeric) |
| `parse_float` | `(string) -> float` | parse a float (0 if not numeric) |
| `f64_bits` | `(float) -> int` | reinterpret a `float`'s IEEE-754 bits as an `int` (for byte serialization) |
| `f64_from_bits` | `(int) -> float` | the inverse: an `int` bit pattern → `float` |
| `exit` | `(int) -> ` | terminate the process with a status code |
| `flush` | `() -> ` | flush buffered stdout (for prompt output through a pipe) |
| `raw_mode` | `(int) -> int` | put the terminal in cbreak/no-echo mode (`1`) or restore it (`0`); pair them and restore before exit (TUIs/games) |
| `read_key` | `() -> string` | non-blocking single-key read: a 1-char string, or `""` if no key is waiting (needs `raw_mode` for live input) |
| `read_file` | `(string) -> string` | read a whole file ("" on error) |
| `write_file` | `(string, string) -> int` | write a file (bytes written; -1 on error) |
| `list_dir` | `(string) -> []string` | directory entries (excludes `.`/`..`) |
| `mkdir` | `(string) -> int` | create a directory (0 ok; -1 on error) |
| `len` | `(string\|slice\|map) -> int` | length |
| `str` | `(int\|float\|bool\|string) -> string` | format a value: a number, a bool (`"true"`/`"false"`), or a string (identity) |
| `int` | `(number) -> int` | truncate to int |
| `float` | `(number) -> float` | `int` → `float` (identity on `float`); there is no implicit `int`→`float`, so a concrete `int` needs this to enter float arithmetic |
| math | `(number[, number]) -> float` | native libm (linked `-lm` only when used): `sin` `cos` `tan` `asin` `acos` `atan` `atan2` `sqrt` `cbrt` `pow` `exp` `log` `log2` `log10` `floor` `ceil` `round` `trunc` `abs` `fmod` `hypot`, and `pi()`. Numeric in, `float` out. An `extern` of the same name shadows the builtin. |
| `noise2`/`noise3` | `(number, ...) -> float` | Perlin gradient noise (2D / 3D), deterministic, ~`[-1,1]`, smooth. Layer it (fbm) for procedural terrain. Links `-lm`; emitted only when used. |
| `append` | `([]T, T) -> []T` | grow a slice |
| `has`, `delete` | `(map, K) -> bool` / `-> ` | membership / removal |
| `keys` | `(map[K]V) -> []K` | a map's keys |
| `json` | `(any) -> string` | serialize to JSON |
| `parse` | `(string, T{}) -> T` | parse JSON into `T`'s type (witness) |
| `json_get` | `(string, string) -> (string, string)` | value at a jq-style path → `(value, err)`; `value` is raw JSON text, `err` is `""`/`"notfound"`/`"path"`/`"parse"`. Multi-assign only. |
| `http_body` | `(string) -> string` | body of an HTTP message |
| `substr` | `(string, int, int) -> string` | substring |
| `index` | `(string, string) -> int` | first index, or `-1` |
| `contains`, `has_prefix`, `has_suffix` | `(string, string) -> bool` | text tests |
| `charat` | `(string, int) -> string` | 1-character string |
| `to_upper`, `to_lower`, `trim` | `(string) -> string` | case / whitespace |
| `replace` | `(string, string, string) -> string` | replace all |
| `split` | `(string, string) -> []string` | split |
| `join` | `([]string, string) -> string` | join |
| `base64_encode` | `(string) -> string` | base64-encode text (standard, padded) |
| `base64_decode` | `(string) -> string` | base64-decode (lenient: standard + url-safe; ignores padding) |
| `bytes` | `(string) -> bytes` | a `bytes` value from a string's raw bytes |
| `bytes_str` | `(bytes) -> string` | `bytes` → string (NUL-terminated; truncates at an embedded `0`) |
| `to_hex` | `(bytes) -> string` | lowercase hex of a `bytes` value |
| `from_hex` | `(string) -> bytes` | parse hex → `bytes` (skips non-hex chars) |
| `byte_at` | `(bytes, int) -> int` | byte value 0–255 at an index (`-1` if out of range) |
| `bytes_sub` | `(bytes, int, int) -> bytes` | sub-range `[start, end)` |
| `bytes_concat` | `(bytes, bytes) -> bytes` | concatenate two `bytes` values |
| `rand_bytes` | `(int) -> bytes` | `n` cryptographically-random bytes (CSPRNG) |
| `sha256_bytes` | `(bytes) -> bytes` | SHA-256 of binary → 32-byte digest (binary-safe) |
| `hmac_sha256_bytes` | `(bytes, bytes) -> bytes` | HMAC-SHA256(key, msg) → 32 bytes |
| `hkdf_sha256` | `(bytes, bytes, bytes, int) -> bytes` | HKDF-SHA256(ikm, salt, info, length) |
| `pbkdf2_sha256` | `(bytes, bytes, int, int) -> bytes` | PBKDF2-HMAC-SHA256(password, salt, iterations, dklen) |
| `x25519_pub` | `(bytes) -> bytes` | X25519 public key from a 32-byte private key |
| `x25519_shared` | `(bytes, bytes) -> bytes` | X25519 ECDH shared secret (my private, their public) |
| `ed25519_pub` | `(bytes) -> bytes` | Ed25519 public key from a 32-byte seed |
| `ed25519_sign` | `(bytes, bytes) -> bytes` | Ed25519 signature (seed, msg) → 64 bytes |
| `ed25519_verify` | `(bytes, bytes, bytes) -> bool` | Ed25519 verify (pub, msg, sig) |
| `aes_gcm_encrypt` | `(bytes, bytes, bytes, bytes) -> bytes` | AES-GCM (key, iv, plaintext, aad) → ciphertext‖16-byte tag |
| `aes_gcm_decrypt` | `(bytes, bytes, bytes, bytes) -> bytes` | AES-GCM decrypt → plaintext (empty `bytes` on auth failure) |
| `aes_cbc_encrypt` | `(bytes, bytes, bytes) -> bytes` | AES-CBC (key, iv, plaintext), PKCS#7 padded |
| `aes_cbc_decrypt` | `(bytes, bytes, bytes) -> bytes` | AES-CBC decrypt → plaintext (empty on bad padding) |
| `xeddsa_sign` | `(bytes, bytes, bytes) -> bytes` | XEdDSA signature over Curve25519 `(priv32, msg, random64)` → 64 bytes (Signal/WhatsApp identity signatures); links libsodium |
| `xeddsa_verify` | `(bytes, bytes, bytes) -> bool` | XEdDSA verify `(curve25519 pub32, msg, sig64)`; links libsodium |
| `keccak256` | `(bytes) -> bytes` | Keccak-256 (Ethereum's hash — NOT NIST SHA3-256, different padding) → 32-byte digest |
| `secp256k1_pubkey` | `(bytes) -> bytes` | secp256k1 public key from a 32-byte private key → 65-byte uncompressed point (`0x04‖X‖Y`) |
| `secp256k1_sign_recoverable` | `(bytes, bytes) -> bytes` | secp256k1 ECDSA sign `(priv32, hash32)` → 65-byte `r‖s‖v` (EIP-2 canonical low-S; `v` is 27/28) |
| `secp256k1_recover` | `(bytes, bytes) -> bytes` | secp256k1 ECDSA recover `(hash32, sig65)` → the 65-byte uncompressed pubkey (empty `bytes` if invalid); same math as Solidity's `ecrecover` |
| `url_encode` | `(string) -> string` | percent-encode for URLs (RFC 3986: keeps `A-Za-z0-9-._~`, encodes the rest, space → `%20`) |
| `url_decode` | `(string) -> string` | percent-decode a URL component (lenient: `+` → space, malformed `%XX` passes through) |
| `sha256` | `(string) -> string` | SHA-256 of text, lowercase hex |
| `hmac_sha256` | `(string, string) -> string` | HMAC-SHA256(key, message), lowercase hex |
| `sqlite_open` | `(string) -> int` | open/create a SQLite db file → handle (0 on fail); `:memory:` for in-memory |
| `sqlite_exec` | `(int, string[, []string]) -> int` | run SQL with no result; an optional `[]string` binds the `?` placeholders (injection-safe); 0 on success |
| `sqlite_query` | `(int, string[, []string]) -> string` | run a SELECT → JSON array of row objects; an optional `[]string` binds the `?` placeholders; composes with `json_get` |
| `sqlite_close` | `(int) -> int` | close the database |
| `regex_match` | `(string, string) -> bool` | does a POSIX ERE pattern match anywhere in s |
| `regex_find` | `(string, string) -> string` | first ERE match in s (`""` if none) |
| `regex_groups` | `(string, string) -> []string` | first match's groups: `[0]` whole, `[1..]` captures (`[]` if none) |
| `regex_replace` | `(string, string, string) -> string` | replace all ERE matches in s with repl; `repl` is inserted literally — no `\1`/`$1` backreferences |

An invalid ERE pattern (a `regcomp` failure) is not an error — there's no error channel, so each `regex_*` builtin silently falls back to its benign default: `regex_match`→`false`, `regex_find`→`""`, `regex_groups`→`[]`, `regex_replace`→`s` unchanged. See `examples/complex/grep.mfl`.
| `sleep` | `(int) -> ` | pause (milliseconds) |
| `dial` | `(string, int) -> int` | connect to host:port; an fd, or -1 on failure |
| `peer_addr` | `(int) -> string` | the remote address of a connected socket fd |
| `socket_timeout` | `(int, int) -> int` | set a read/write timeout (milliseconds) on a socket fd |
| `listen`, `accept` | `(int) -> int` | open / accept on a TCP socket |
| `read`, `write` | `(int[, string]) -> string\|int` | socket/fd I/O — `read` is one `read(2)` of up to 65535 bytes, not a whole message; loop `read_bytes` (NUL-safe) to reassemble a complete request (see `framework/machweb.src`'s `read_request_bytes`, and issue #91) |
| `close` | `(int\|chan) -> ` | close a socket/fd, or a channel (dispatched by argument) |
| `https_get` | `(string) -> string` | HTTPS GET over TLS; response body ("" on error) |
| `https_post` | `(string, string) -> string` | HTTPS POST (JSON body) over TLS; response body |
| `http_get` | `(string) -> (int, string, string)` | GET (plain `http://` or `https://`) returning `(status, body, err)`; `err==""` ⇒ a response, else `"dns"`/`"connect"`/`"tls"`. Multi-assign only. |
| `http_request` | `(string, string, []string, string) -> (int, string, string)` | authenticated HTTPS: `(method, url, header lines, body)` → `(status, body, err)`. Each header is a `"Key: Value"` line (e.g. `"Authorization: Bearer …"`); caller owns `Content-Type`. Multi-assign only. |
| `wss_open` | `(string) -> int` | open a `wss://` WebSocket; a connection handle, or 0 on failure |
| `wss_send` | `(int, string) -> int` | send a text message on a WebSocket |
| `wss_recv` | `(int) -> string` | next message (blocks); `""` on close (auto ping/pong) |
| `wss_send_bin` | `(int, bytes) -> int` | send a binary message (opcode `0x2`) — NUL-safe |
| `wss_recv_bin` | `(int) -> bytes` | next message as `bytes` (blocks); empty `bytes` on close |
| `wss_close` | `(int) -> int` | send close and tear down the connection |
| `tls_server_ctx` | `(string, string) -> int` | load a cert+key (PEM files) → a server TLS context handle (0 on fail); for terminating HTTPS/TLS yourself, see `serve_tls` |
| `tls_accept` | `(int, int) -> int` | `(ctx, fd)` — complete a server-side TLS handshake on an `accept()`'d fd → a tls handle (0 on fail) |
| `tls_client_fd` | `(int, string) -> int` | `(fd, hostname)` — the STARTTLS primitive: upgrade an already-connected, plaintext-negotiated fd to a verified TLS handle in place (0 on fail) |
| `tls_read` | `(int) -> string` | read one chunk from a tls handle (blocks; `""` at EOF/error) |
| `tls_read_bytes` | `(int) -> bytes` | read one chunk from a tls handle as raw bytes, NUL-safe |
| `tls_write` | `(int, string) -> int` | write to a tls handle |
| `tls_write_bytes` | `(int, bytes) -> int` | write raw bytes to a tls handle, NUL-safe |
| `tls_close` | `(int) -> int` | shut down a tls handle and close its underlying fd |

---

## 9. Multiple return values

A function may return several values; a call destructures them:

```go
func divmod(a, b) { return a / b, a % b }
q, r := divmod(17, 5)
v, ok := lookup(m, k)     // (value, ok) idiom
a, b = b, a              // parallel assignment, RHS evaluated first
```

A multi-value call may appear only as the sole right-hand side of a
multi-assignment. `_` discards a value. A function returning ≥2 values compiles
to a generated result struct.

**Named returns.** A function may name its return values in the signature; they
become zero-initialized locals, and a bare `return` (or falling off the end)
yields their current values:

```go
func divmod(a, b) (q, r) {
    q = a / b
    r = a % b
    return            // returns q, r
}
```

---

## 10. Concurrency

- `go f(args)` runs a function call in a new goroutine (a POSIX thread).
- **Channels:** `make(chan T)` creates a channel; `ch <- v` sends; `<-ch`
  receives, blocking until a value is available. The element type is inferred.
- `sleep(ms)` suspends the current goroutine.

Channels are an unbounded FIFO: sends do not block; a receive blocks until a
value arrives.

---

## 11. Functions, closures, and generics

### 11.1 Function values and closures

A `func(params) { ... }` literal is a value that can be stored, passed, and
returned. It **captures** free variables from the enclosing scope **by
reference** (as in Go): a captured variable lives in a shared heap cell, so
assignments made through the closure are visible to the enclosing scope and to
any other closure over the same variable, and vice versa. This makes the
mutable-state idiom work — e.g. a `counter()` that returns a closure
incrementing a captured local on each call. Function values hold a single return
value (or none). Compilation is by closure conversion (lambda-lifting): a
literal becomes a top-level function plus an environment of pointers to the
captured cells.

### 11.2 Generics

Functions are **implicitly generic**: because parameter types are inferred, a
function imposes no constraint beyond its use, so the same source function works
at many types. Each call is compiled by **monomorphization** — the compiler
emits one specialized native function per concrete call-site signature
(deduplicated). There is no boxing. Recursion is monomorphic (one concrete type
per instantiation).

```go
func id(x) { return x }
id(42); id("hi"); id(3.14)   // → three native functions
```

---

## 12. Execution and memory

- A program runs by calling `main`. The process exits 0 on normal completion.
- **Memory** is managed by a per-goroutine **arena**: value buffers (strings,
  slice backings, closure environments) are allocated from the running
  goroutine's arena and reclaimed in bulk when that goroutine finishes. The main
  goroutine's arena lives for the whole program. This bounds the memory of a
  long-running concurrent server — each request handler runs in its own
  goroutine and frees everything it allocated on return.
  - **Channels and `go` call arguments deep-copy heap data across this
    boundary:** a value sent over a channel, or passed as an argument to a `go`
    call — a `string`, a slice, a map, or a struct containing any of these,
    nested arbitrarily — is copied out of the source arena and rebuilt in the
    destination goroutine's arena, so it stays valid even after the source
    goroutine finishes. (Strings take a fast offset-copy path; values containing
    a slice or map go through a JSON round-trip.) Fixed in v0.104.0 for `go`
    arguments — see #310; the argument, not just a later channel send of it, is
    now safe.
  - *Caveat:* a value allocated in one goroutine and shared with another by other
    means (stored in a map outliving the sender, or captured by a closure that is
    itself stored somewhere long-lived rather than passed directly to `go`) may
    be reclaimed while still referenced. Keep such values in the receiver's
    scope.
- A **scoped arena** block, `arena { ... }`, installs a fresh arena for the
  duration of the block and frees everything allocated inside it when the block
  ends. This bounds the memory of a *single* long-lived goroutine that allocates
  per iteration (the one case the per-goroutine arena alone does not cover):
  wrap the loop body in `arena { ... }` and peak memory stays flat instead of
  growing without limit. Blocks nest. The contract is that **nothing allocated
  inside the block escapes it** — a value computed inside and read after the
  block (assigned to an outer variable, returned, sent on a channel) dangles, as
  with a stack frame. Scalars (ints, floats, bools) are not heap-allocated, so
  accumulating them across the block is always safe.
- By default, integer overflow wraps (two's complement) and division by zero /
  out-of-bounds slice access are undefined (they follow the generated C).
- Building with **`--safe`** inserts runtime checks: a slice index out of range,
  integer division/modulo by zero, or integer `+`/`-`/`*` overflow prints a
  `panic:` message to stderr and exits non-zero. `--safe` is opt-in; the default
  build has zero check overhead.

---

## 13. Compilation model

```
.mfl (canonical text) ──▶ parse ──▶ lambda-lift ──▶ infer + monomorphize ──▶ emit C ──▶ cc -O2 ──▶ native binary
                                                                                   └──▶ zig cc (wasm32-wasi) ──▶ .wasm
```

- **Inference** is unification over a union-find; deferred resolution handles
  `x[i]`, `x.f`, and `range` once the base type is known.
- **Monomorphization** instantiates the reachable call graph from `main` and from
  every `export func` root, specializing each function per concrete type and
  deduplicating identical instances.
- **Codegen** emits one C function per instance with unboxed value types, plus a
  small C runtime (slices, maps, channels, closures, sockets, JSON, strings).
  `https_get`/`https_post` and `wss_open`/`wss_send`/`wss_recv`/`wss_close`
  additionally emit a TLS runtime (HTTPS and/or RFC 6455 WebSocket framing over a
  shared TLS core) and link OpenSSL (`-lssl -lcrypto`) — but only when used, so
  programs that touch neither stay libc-only.
- **Targets.** The default target is a native binary via `cc -O2` (the C compiler
  defaults to `cc`; set `CC` to override it, e.g. `CC=clang machin build app.mfl
  -o app`). `machin build
  --target wasm` instead emits a WebAssembly module (`wasm32-wasi`, reactor model)
  via `zig cc` — zig bundles clang + a wasi-libc, so it is a single-binary C→wasm
  toolchain (override with `ZIG=`). For the wasm target: each `export func`
  becomes a wasm export (under its source name, via an `export_name` attribute)
  and a reachability root, so a module needs no `main`; a headerless `extern
  "<mod>"` becomes a wasm import the host supplies (`import_module`/`import_name`);
  and the POSIX socket/tty runtime is emitted only when used (so a frontend module
  references no `socket()`/`termios`). machin ints are i64 (`BigInt` across the JS
  boundary); strings are pointers into the exported `memory`.

---

## 14. Grammar (informal)

```
Program     = { Decl } .
Decl        = FuncDecl | TypeDecl .
TypeDecl    = "type" ident "struct" "{" { ident TypeName } "}" .
FuncDecl    = "func" ident "(" [ identList [ "..." ] ] ")" [ "(" identList ")" ] Block .
TypeName    = "int" | "float" | "bool" | "string" | ident
            | "[]" TypeName | "map" "[" TypeName "]" TypeName | "chan" TypeName .
Block       = "{" { Stmt } "}" .
Stmt        = Decl? | Assign | If | Loop | Return | Send | Go | Select | Arena | ExprStmt
            | "break" | "continue" .
Assign      = identList ( ":=" | "=" ) exprList .
If          = "if" Expr Block [ "else" ( If | Block ) ] .
Loop        = ( "while" | "for" ) [ Expr ] Block
            | "for" ident [ "," ident ] ":=" "range" Expr Block .
Return      = "return" [ exprList ] .
Send        = Expr "<-" Expr .
Go          = "go" Call .
Arena       = "arena" Block .
Select      = "select" "{" { "case" Comm ":" { Stmt } } [ "default" ":" { Stmt } ] "}" .
Comm        = ident [ "," ident ] ":=" "<-" Expr | "<-" Expr | Expr "<-" Expr .
Expr        = ... operators, calls, indexing, field access, literals,
              FuncLit, make, "<-" Expr ... .
FuncLit     = "func" "(" [ identList ] ")" Block .
```

---

## 15. Foreign functions (C FFI)

An `extern` declaration names foreign C functions and how to compile and link
against them. It is a single top-level declaration (one line, like any other):

```
extern "m" { header "math.h" link "m" fn sqrt(float) float fn pow(float, float) float }
```

- `"m"` — an informational library name.
- `header "h.h"` — emits `#include <h.h>` so the real C prototype is in scope.
  If omitted, machin emits a prototype from the declared signature instead.
- `link "l"` — passes `-l<l>` to the C compiler. May be repeated; the libraries
  are emitted in declaration order (e.g. `link "raylib" link "GL" link "m"` for a
  library with transitive dependencies). `link ":libname.a"` forces a static
  archive over a shared `.so`.
- `cflags "..."` — passes extra flags (e.g. `-I`/`-L` paths) to the C compiler.
- `cstruct Name { field ctype ... }` — declares a C struct's layout (Phase 2).
  machin synthesizes a matching MFL struct `Name` (so MFL can construct and
  field-access it) and marshals between the MFL value and the C struct by value
  at the boundary. Field types are sized C scalars (below) **or another declared
  `cstruct`** — nested by-value structs marshal recursively, so e.g. raylib's
  `Camera3D` (three `Vector3`s + scalars) is expressible and constructible
  (`Camera3D{Vector3{...}, ...}`).
- `cstruct Name {}` — an **opaque handle** (Phase 3): an empty body declares a
  by-value C type (from the `header`) that machin holds and passes back **without
  naming its fields**. This is for by-value structs that contain pointers and so
  can't be a numeric `cstruct` — e.g. raylib's `Sound`/`Music`/`Font`. MFL can
  receive one from a `fn`, store it (in a variable or `[]Name`), and pass it to
  another `fn`; it cannot construct or field-access it. (Distinct from `ptr`,
  which is a single pointer held as an `int`; an opaque `cstruct` is the whole
  by-value aggregate.)
- `fn Name(t, ...) ret` — a foreign function. Parameter and return types are FFI
  scalar types, or the name of a declared `cstruct` (numeric or opaque); a
  missing return type means `void`. Two pointer param forms: **`*T`** (`T` a C
  type from the header) — the MFL argument is a pointer (an `int`) and the call
  **dereferences it, passing the pointed-to `T` by value** (`fn LoadModelFromMesh(*Mesh)`);
  and **`T*`** (`T` a declared `cstruct`) — **inout**: the MFL argument is a
  cstruct *variable*, marshaled to a C temporary, passed **by pointer**, and the
  modified struct **written back** after the call (`fn UploadMesh(Mesh*, bool)`).
  (To pass a raw pointer itself, use `ptr`.)
- A `cstruct` **field** may also be **`ptr`** — a pointer, held as an `int` in MFL
  and cast through `void*` at the boundary (which C converts to the real field
  type, `float*` etc.). This lets MFL declare a struct like raylib's `Mesh`
  (pointer fields to GPU buffers) and let the C compiler lay it out, instead of
  poking raw bytes at hard-coded offsets.

**Raw memory** (pointers are `int`s, like `ptr`) lets MFL build C buffers and
structs to hand to a foreign function: `alloc(n) -> int` (n **zeroed** bytes),
`free(p)`, `poke_f32`/`poke_i32`/`poke_u8`/`poke_u16`/`poke_ptr(p, byteOffset, v)`,
`peek_f32`/`peek_i32(p, byteOffset)`, and `ptr_str(p) -> string` (read a
NUL-terminated C string at `p`). The pattern for a GPU mesh: `poke` the
vertex/colour arrays and a `Mesh` struct into `alloc`'d memory, `UploadMesh(p, ...)`
(a `ptr` param — `void*` converts to `Mesh*`, and the call writes the VAO/VBO ids
back), then `LoadModelFromMesh(p)` (a `*Mesh` param). Struct offsets are
layout-specific — pin the C library version.

**FFI scalar types** → C: `int`/`i64`→`int64_t`, `i32`→`int32_t`, `i16`→`int16_t`,
`i8`→`int8_t`, `u64`→`uint64_t` … `u8`→`uint8_t`, `float`/`f64`→`double`,
`f32`→`float`, `bool`→`int`, `string`→`const char*`, `ptr`→`void*`. The sized
types matter for ABI correctness (e.g. raylib takes 32-bit `float`/`int`). In
MFL, every integer width is `int`, `f32`/`f64` are `float`, and **`ptr` is an
opaque handle held as an `int`** (the pointer value, round-tripped through
`intptr_t`) that MFL passes back to C but never dereferences — for `FILE*`,
window/texture handles, etc. `ptr` and `string` may not be `cstruct` fields
(only numeric scalars).

A call to a declared name compiles to a **direct C call**: scalar arguments are
cast to their C type, `ptr` arguments to `void*`, and struct arguments are
marshaled into the C layout (struct/`ptr` returns are converted back). The call
is type-checked against the declared arity/types like any other.

Phases 1–3 (scalars, **by-value structs**, **opaque handles/pointers**) are
implemented. Callbacks (passing an MFL closure as a C function pointer) are a
future phase. The FFI boundary is unchecked C: a value an MFL string passes to C
is arena-allocated, so a C function that retains the pointer past the arena's
lifetime would dangle — pass copies or keep it in scope.

---

## 16. Status and non-goals

Implemented: the entire surface above, including arena memory management with
scoped `arena { }` blocks (§12), named return values (§9), variadic parameters
(§5.2), by-reference closure capture (§11.1), opt-in bounds/overflow checks
(`--safe`), and C FFI scalars + by-value structs + opaque handles (§15). Not yet
implemented: polymorphic recursion, an automatic tracing GC, and FFI callbacks
(MFL closures as C function pointers). `nil` is reserved and parses as a
literal (§6) but is **not implemented**: it is rejected at type-check, reserved
for future slice/map/chan/func-optional support. These are refinements, not
core gaps.
