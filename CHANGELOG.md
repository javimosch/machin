# Changelog

## Unreleased

- **Native WebSocket — `wss_open`, `wss_send`, `wss_recv`, `wss_close`.** A
  `wss://` client (RFC 6455) over real TLS, no subprocess. `wss_open(url)` does
  the HTTP/1.1 Upgrade handshake and returns a connection handle; `wss_send`
  masks and writes a text frame; `wss_recv` blocks for the next message,
  reassembling fragments and transparently answering pings and handling close;
  `wss_close` tears down. Built on a shared TLS core refactored out of the HTTPS
  client (one process-global `SSL_CTX`), emitted and linked (`-lssl -lcrypto`)
  only when used. Surfaced dogfooding a streaming scraper that had to shell out
  to `websocat` — this retires that crutch too: a Polymarket CLOB stream now runs
  fully native (`https_get` to resolve the token, `wss_*` to stream).

## v0.9.0

- **Native TLS — `https_get` and `https_post`.** machin's biggest networking
  gap is closed: an HTTPS client over real TLS (OpenSSL), no subprocess. `https_get(url)`
  and `https_post(url, jsonBody)` return the response body, handling cert
  verification (SNI + hostname), `Content-Length`, chunked transfer-encoding, and
  redirects. The OpenSSL runtime is emitted and linked (`-lssl -lcrypto`) **only
  when used**, so TLS-free programs keep their libc-only footprint. Surfaced
  building a Polymarket scraper that had to shell out to `curl`/`websocat` because
  machin couldn't open a TLS socket — this retires the `curl` crutch for REST.

## v0.8.0

More dogfooding: building a streaming WebSocket scraper drove these in.

- **`break` and `continue`.** Loop control was missing entirely — the only way
  out of a `for`/`while` was a flag variable. `break` exits the innermost loop,
  `continue` skips to its next iteration; both work in `for cond`, `for {}`, and
  `range` loops (range increments live in the C `for` clause, so `continue` is
  safe). Surfaced writing hand-rolled JSON/stream parsers in MFL.
- **`encode` — string- and comment-aware function splitting.** `splitFunctions`
  counted every `{`/`}` to find declaration boundaries, including braces inside
  string literals and `//` comments. Any function emitting JSON (`"{...}"`) or
  searching for a brace (`index(s, "}")`) failed with `unbalanced braces`. It now
  tracks string state and stops at `//`.

## v0.7.0

Dogfooding: real tools drove these in. A health checker added networking +
timing + parsing; a static-site generator added file I/O and caught a parser
bug. See [awesome-machin](https://github.com/javimosch/awesome-machin).

- **Outbound networking — `dial(host, port)`.** Connect a TCP socket to a remote
  host (DNS-resolved via `getaddrinfo`), returning an fd used with the existing
  `read`/`write`/`close`. machin was server-only (`listen`/`accept`); `dial` makes
  it a client too — HTTP clients, health checkers, anything that reaches out.
  Surfaced and filled while building a real tool (the "build real things" goal).
- **`now_ms()` and `parse_int()`.** Wall-clock milliseconds (for measuring
  latency) and string→int parsing (`0` on non-numeric). Both surfaced building
  the same tool — a concurrent HTTP health checker.
- **File I/O — `read_file`, `write_file`, `list_dir`, `mkdir`.** Read/write whole
  files, list a directory (excludes `.`/`..`), make a directory. Native builtins
  (no FFI), surfaced building a static-site generator.
- **Parser fix — string literals equal to a structural token.** A string like
  `")"` was mistaken for the closing delimiter, so `index(s, ")")` failed to
  parse; value-list loops are now punctuation-aware. Caught by the SSG.
- **CLI builtins — `args()`, `env()`, `now()`.** `args()` returns the
  command-line arguments (`[]string`; `args()[0]` is the program path) — the
  generated `main` now takes `argc`/`argv`. `env(name)` reads an environment
  variable (`""` if unset). `now()` returns Unix seconds. Together these let MFL
  programs be real CLIs (subcommands, flags, `$PORT`, uptime) — the basis for a
  machin-based CLI/server boilerplate.

## v0.6.0

- **C FFI (Phases 1–3).** An `extern "lib" { header "..." link "..." cflags "..."
  cstruct T { f ctype ... } fn name(types) ret }` declaration names foreign C
  functions; calls compile to direct C calls and `header`/`link`/`cflags` are
  threaded into `cc`. **Phase 1:** scalar types — `int`/`float`/`bool`/`string`
  plus sized `i8…u64`/`f32`/`f64` (sizes matter for ABI: raylib takes 32-bit
  `int`/`float`). **Phase 2:** `cstruct` declares a C struct's layout; machin
  synthesizes a matching MFL struct and marshals it by value across the boundary
  (pass and return). **Phase 3:** the `ptr` type — an opaque C handle (`void*`,
  e.g. `FILE*` or a window/texture handle) held as an MFL `int` and passed back
  to C, never dereferenced. New `examples/complex/ffi_math.mfl`, `ffi_struct.mfl`,
  and `ffi_ptr.mfl`; the path to the C ecosystem and a native GUI.
- **Native GUI demo — `examples/gui/game_menu.mfl`.** A clickable Start / Settings
  / Exit menu drawn with [raylib](https://www.raylib.com) through the FFI: opens a
  real OpenGL window, draws rectangles/text with a `Color` cstruct, and polls the
  mouse each frame — proving Phases 1–2 are enough to drive a real graphics
  library. `extern` blocks may now have multiple `link` directives, kept in order
  (`-lraylib -lGL -lm -lpthread -ldl -lrt -lX11`). A GUI binary links the system
  graphics stack and needs a display — not a no-deps binary, as with any native GUI.
- **Tightened canonical form (token-minimization).** The canonical `.mfl` now
  drops whitespace adjacent to operators/punctuation (`fib(n - 1)` →
  `fib(n-1)`), keeping only the spaces the lexer needs between word tokens. Zero
  semantic change; ~13% fewer agent tokens to write/edit the corpus, measured
  with the new `tools/tokmin.py`. The same harness showed the *intuitive*
  minimizations are dead ends — `func`→`fn` saves **0** tokens (both are single
  tokens already) and `println`→`pln` is *worse* (abbreviations fragment) — so
  whitespace is where the win is.

## v0.5.0

- **Plain text is the source of truth.** The `.mfl` form is now canonical plain
  text — one normalized function per line — instead of base64. The reason is the
  language's own north star: measured with `tools/tokcost.py`, base64 costs an
  agent ~2.5× the output tokens to write/edit (and ~9× for a one-character edit),
  taxing the very machine-speed it was meant to signal. Text is greppable,
  diffable, and editable in place. `machin run` still reads the base64 form, now
  produced on demand by **`machin pack`** for distribution. Machine-first now
  means *shaped for machine authoring* (terse, inferred, canonical,
  function-addressable), not *encoded*.
- **`input()` builtin** — read a line from stdin (`() -> string`), enabling
  interactive / native desktop CLI programs. New `examples/complex/game_menu.mfl`.
- **`tools/tokcost.py`** — a tiktoken harness that measures the agent write/edit
  token cost of a source form; the instrument behind the plain-text decision.

## v0.4.2

- **Windows binaries.** Releases now also ship `machin-<tag>-windows-amd64.exe`,
  alongside linux/macOS × amd64/arm64. Five prebuilt binaries per release.

## v0.4.1

- **Release automation.** Pushing a `v*` tag now cross-compiles machin for
  linux/macOS × amd64/arm64 (pure Go, static, ~2 MB) and attaches the binaries
  plus `SHA256SUMS.txt` to the GitHub release — no manual upload step.

## v0.4.0

Native-language depth: safety, real closures, and bounded memory — plus the
platform layer (framework, router, `func` type) that landed since v0.3.0.

- **`--safe` build mode.** `machin run|build <file> --safe` inserts runtime
  checks: a slice index out of range, integer division/modulo by zero, or
  integer `+`/`-`/`*` overflow prints a Go-style `panic:` to stderr and exits
  non-zero. Opt-in — the default build keeps zero check overhead.
- **By-reference closure capture.** Closures now capture enclosing variables by
  reference (Go semantics): a captured variable lives in a shared cell, so a
  closure can mutate state that outlives the call that made it. The
  counter/accumulator idiom (`func counter() { n := 0  return func() { n = n + 1
  return n } }`) works, and sibling closures share one cell.
- **Scoped arenas (`arena { }`).** Wrapping a loop body in `arena { ... }`
  reclaims everything allocated inside the block when it ends, keeping a
  long-lived loop's memory flat (measured ~240 MB → ~1.4 MB over a 1M-iteration
  allocating loop). Blocks nest and compose with goroutines and `--safe`.

- **machweb — a web framework written in MFL.** `Request`/`Response` types,
  response builders (`ok_text`/`ok_html`/`ok_json`/`created`/`bad_request`/
  `not_found`), `parse_request`, a `param(path, prefix)` path helper, and
  `serve(port, handler)` which dispatches each request — in its own goroutine —
  to a handler closure `func(Request) Response`. A backend compiles to a single
  native binary with no runtime dependencies. See [`framework/`](framework/).
- **Map-based router.** `new_router()` → `route(r, method, path, handler)` →
  `serve_router(port, r)`. Handlers live in a `map[string]func` keyed by
  `"METHOD PATH"`; routing is method-aware and unmatched requests return `404`.
- **The `func` type.** A function-value type whose signature is inferred by
  unification — it lets closures be stored in slices, maps
  (`make(map[string]func)`), and struct fields. This is what makes a router's
  handler table possible.
- **Multi-file `machin encode`.** `encode` now accepts several source files and
  concatenates them, so a framework and an app compose into one program:
  `machin encode framework/machweb.src myapp.src > app.mfl`.

## v0.3.0

Ergonomics, toward feeling like Go to write:

- **Named return values.** `func divmod(a, b) (q, r) { q = a/b; r = a%b; return }`
  — the named returns are zero-initialized locals; a bare `return` (or falling
  off the end) yields them.
- **Variadic parameters.** A function's last parameter may be variadic
  (`func sum(nums...)`), collecting trailing call arguments into a slice. Call
  with extra args (`sum(1, 2, 3)`) or spread a slice (`sum(xs...)`). Variadics
  are generic — one source function specialized per element type.

## v0.2.1

- **Arena memory management.** Value buffers (strings, slice backings, closure
  environments) are allocated from a per-goroutine arena and reclaimed in bulk
  when the goroutine returns; the main goroutine's arena lives for the whole
  program. This bounds the memory of a long-running concurrent server — under a
  12,000-request load the self-host server's RSS plateaus at ~1.8 MB instead of
  growing unbounded. (Subsystems that free explicitly — channels, maps — keep
  raw allocation.)

## v0.2.0

A consolidation release. MFL grew from a base64 POC interpreter into a
native-compiling backend language with the complete Go-flavored core, plus a
formal specification ([`SPEC.md`](SPEC.md)).

### Language

- **Compilation to native code** — programs are translated to C99 and compiled
  with `cc -O2`; values are unboxed. `fib(40)` runs in ~0.20s, on par with
  hand-written C. (The original tree-walking interpreter was removed.)
- **Static typing by inference** — no annotations; type clashes are compile errors.
- **Composite types** — slices `[]T`, structs (`type T struct { ... }`), and
  maps `map[K]V` (int/string keys), all unboxed.
- **Control flow** — `for cond {}`, `for {}`, `while`, and `for k, v := range x`
  over slices, maps, and strings.
- **Multiple return values** — `return a, b`, destructuring `q, r := f()`,
  parallel assignment, and the comma-ok pattern.
- **Closures & first-class functions** — `func(x){...}` literals with by-value
  capture, higher-order functions (lambda-lifting + closure conversion).
- **Generics** — functions are implicitly generic, specialized per concrete
  call-site type by monomorphization (no boxing, no annotations).
- **Concurrency** — `go` goroutines (pthreads), channels (`make(chan T)`,
  `<-`), and `sleep`.
- **Networking & JSON** — BSD sockets (`listen`/`accept`/`read`/`write`/`close`),
  bidirectional JSON (`json(x)` serialize, `parse(s, T{})` parse), and string
  operations — enough to write a concurrent JSON-over-HTTP API with routing.

### Tooling

- `machin run` / `build` / `build --emit-c` / `encode`.
- `Makefile`, MIT `LICENSE`, `SPEC.md`, and 35 runnable examples.
- 51 Go tests exercising the full surface via the native path.

## v0.1.0

Initial POC: MFL as base64 (one function per line), a tree-walking interpreter,
`run`/`encode`/`decode`, and a first set of examples.
