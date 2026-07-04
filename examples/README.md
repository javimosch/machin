# MFL examples

Every program here is a **`.mfl`** file — canonical plain text, one normalized
function per line. The `.mfl` *is* the source: greppable, diffable,
machine-authored. That's the machine-first point: the human states intent, the
machine reads and writes the code. A dense base64 form is available for
distribution via `machin pack`. Each program is compiled to native code
(through C) and executed.

```sh
machin run examples/complex/primes.mfl          # compile to native + run
machin build examples/complex/primes.mfl -o p   # produce a native binary
machin build examples/complex/primes.mfl --emit-c   # see the generated C
./examples/run.sh                                # run every non-server example
```

## basic/

| program | shows |
|---------|-------|
| `hello`        | minimal program |
| `arithmetic`   | operator precedence, int vs float division |
| `variables`    | `:=`, `=`, `var`, `len`, boolean logic |
| `conditionals` | `if / else if / else` |
| `loops`        | `while`, running sum, countdown |
| `functions`    | parameters, return values, composition |
| `temperature`  | float formulas, authored directly as MFL |
| `stdin_upper`  | `read_stdin()` reads all of stdin verbatim, unlike line-oriented `input()` |

## complex/

| program | shows |
|---------|-------|
| `primes`          | trial-division primality, list primes ≤ N |
| `gcd_lcm`         | Euclidean gcd, derived lcm |
| `collatz`         | 3n+1 sequence length; busiest start < 100 |
| `ackermann`       | deep non-primitive recursion |
| `fast_power`      | exponentiation by squaring |
| `isqrt`           | Newton's-method integer sqrt + perfect-square test |
| `to_binary`       | recursive base conversion via string building |
| `pi_leibniz`      | float arithmetic, Leibniz series for π |
| `perfect_numbers` | proper-divisor sums |
| `sieve`           | Sieve of Eratosthenes over a slice |
| `slices`          | slice literals, `append`, indexing, `len`, in-place reverse |
| `maps`            | `map[string]int` word frequency + `map[int]string` lookup, `has`/`delete`/`keys` |
| `ranges`          | `for k, v := range` over slices, strings, and maps |
| `multi_return`    | multiple return values, comma-ok `(v, ok)`, `_`, parallel swap |
| `named_returns`   | named return values, bare `return`, fall-through |
| `variadic`        | variadic parameters: collect, spread (`xs...`), fixed+variadic |
| `closures`        | capturing lambdas, higher-order functions, IIFE |
| `generics`        | one source function specialized at int / string / float (monomorphization) |
| `structs`         | `type` declaration, struct literals, field access, `[]struct` of records |
| `goroutines`      | `go` spawns concurrent workers; `sleep` waits |
| `channels`        | fan-in worker pool — goroutines communicate over a channel |
| `http_server`     | concurrent TCP/HTTP server — `go handle(conn)` per request |
| `json`            | `json()` serialization of scalars, slices, structs, maps |
| `json_api`        | JSON-over-HTTP API — each request returns JSON-serialized structs |
| `json_parse`      | `parse(s, T{})` — JSON into struct/slice/map/scalar, with round-trips |
| `json_get`        | `json_get(json, path)` — jq-style path lookup, `(value, err)` multi-assign, all `err` cases |
| `json_echo_api`   | POST JSON → parse into a struct → echo it back as JSON |
| `strings`         | string ops (`split`/`join`/`substr`/`index`/`replace`/…) + request-line parsing |
| `grep`            | `regex_match`/`regex_find`/`regex_groups`/`regex_replace` |
| `time`            | `time_make`/`time_format`/`time_fields` — build, format, and round-trip a timestamp |
| `bytes`           | `bytes` type: construct, `to_hex`/`from_hex`, `byte_at`, `bytes_sub`, `bytes_concat`, `bytes_str`; NUL-safe vs string |
| `sha256`          | `sha256(s)` → lowercase hex; `hmac_sha256(key, msg)` → lowercase hex; webhook signature verification |
| `crypto`          | OpenSSL crypto suite: SHA-256, HMAC, AES-GCM round-trip (`ct\|\|tag` layout), Ed25519 sign/verify |
| `base64`          | `base64_encode`/`base64_decode`: standard padded encode, lenient decode (standard + URL-safe alphabets, ignores padding) |
| `url_encode`      | `url_encode`/`url_decode`: RFC 3986 percent-encoding round-trip, `+`→space, malformed `%XX` passthrough |
| `http_client_api` | HTTP client: `http_get` multi-assign + error branch, `http_request` with auth/Accept headers (real network calls) |
| `router_api`      | HTTP router — dispatch by method+path, extract path params |
| `sqlite_crud`     | SQLite: `sqlite_open`/`sqlite_exec`/`sqlite_query`/`sqlite_close` — in-memory CRUD with parameterized queries, `parse()` row decode, and `json_get` single-field access |
| `arena`           | scoped `arena{}` allocation |
| `counter`         | closure/state counter |
| `ffi_math`        | C FFI — calling external C functions (scalars) |
| `ffi_ptr`         | C FFI — opaque `ptr` handles |
| `ffi_struct`      | C FFI — by-value `cstruct` |
| `game_menu`       | `input()` + interactive menu |

`http_server`, `json_api`, `json_echo_api`, and `router_api` are servers — they
run forever (or hold a listening socket open), so `run.sh` skips any file
matching `*server*` or `*_api*`. Run them directly:

```sh
machin run examples/complex/http_server.mfl
# in another shell:
curl -i http://localhost:48080/
```

## bench/

| program | shows |
|---------|-------|
| `fib`   | `fib(40)` — native MFL runs neck-and-neck with hand-written C |

## gui/

| program | shows |
|---------|-------|
| `game_menu` | native raylib desktop GUI (clickable Start/Settings/Exit menu) driven through machin's C FFI |

Unlike the rest of the catalog, this is **not** a no-dependency binary — it links
raylib + `libGL`/`libX11` and needs a display. See
[`examples/gui/README.md`](gui/README.md) for build/run instructions.
