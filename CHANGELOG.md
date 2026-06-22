# Changelog

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
