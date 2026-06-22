<p align="center">
  <img src="https://img.shields.io/badge/version-0.4.2-blue" alt="Version">
  <img src="https://img.shields.io/badge/license-MIT-green" alt="License">
  <img src="https://img.shields.io/badge/go-1.22-00ADD8" alt="Go">
  <img src="https://img.shields.io/badge/backend-C%20%E2%86%92%20native-orange" alt="Native">
</p>

<h1 align="center">machin ⎯ Machine-First Language</h1>

<p align="center">
  <b>A backend language based on Go — shaped for machines, not humans.</b><br>
  <b>Minimal syntax, no type annotations, one canonical function per line.</b>
</p>

> Think: "Go, but stripped to what an agent needs to write code fast, and it compiles to native code at C speed."

## ⚡ TL;DR

> A statically-typed language designed for low agent write/edit cost: terse, type-inferred, one normalized function per line. The `.mfl` source is plain canonical text — greppable and diffable. It compiles to native machine code through C.

📖 [Landing page](https://javimosch.github.io/machin/) · full reference: [`SPEC.md`](SPEC.md) · language tour: [`docs/LANGUAGE.md`](docs/LANGUAGE.md) · web framework: [`framework/`](framework/) · self-hosted server: [`selfhost/`](selfhost/)

```bash
# A program is one normalized function per line of plain text:
cat examples/demo.mfl
#   func fib(n) { if n < 2 { return n } return fib(n - 1) + fib(n - 2) }
#
#   func main() { println(fib(10)) }

# Compile to native + run
machin run examples/demo.mfl

# Compile to a standalone native binary
machin build examples/complex/primes.mfl -o primes && ./primes

# Inspect the C the compiler emits
machin build examples/demo.mfl --emit-c

# Emit the dense base64 "packed" form (for distribution); run reads it too
machin pack examples/demo.mfl > demo.packed.mfl
```

👉 Machine-first by *design*, not by encoding — terse, inferred, canonical, function-addressable
👉 Statically typed by inference — no annotations
👉 Compiles to native code via `cc -O2` — fib(40) runs neck-and-neck with hand-written C

## The Problem

Programming languages are designed around human ergonomics: type annotations, formatting rules, indentation, ceremony. When the entity writing the code is an agent, much of that is just **tokens it has to emit** — and tokens are the agent's latency and cost:

- **Ceremony is overhead** — formatting choices, boilerplate, and annotations cost output tokens an agent spends on every edit
- **Inference beats annotation** — if the type is recoverable, writing it is wasted tokens
- **Interpreted or VM languages trade speed for convenience** — convenience the machine doesn't need
- **The form should be measured, not asserted** — machin optimizes the one thing that matters for agents (write/edit token cost) and *measures* it ([`tools/tokcost.py`](tools/tokcost.py))

## The Solution

machin (the toolchain) compiles **MFL** (the language):

- **Machine-first** — the language is shaped for cheap agent authoring: terse, type-inferred, one normalized function per line. The source is plain canonical text (greppable, diffable); the human states intent and agents write the code. Measured, this costs an agent ~2.5× fewer tokens to write/edit than a base64 form ([`tools/tokcost.py`](tools/tokcost.py)).
- **Static types, zero annotations** — types are inferred by unification, so the surface stays minimal
- **Native performance** — MFL emits C and hands it to `cc -O2`, landing on the same machine code class as C / Rust / Zig for scalar work
- **One function = one addressable line** — tooling can ship, cache, or rewrite a single function without touching the rest of the file

---

## ⚡ Quick Start

```bash
# Build the toolchain
make build           # → bin/machin   (or: go build -o machin .)

# Run a program (compile to native + execute)
bin/machin run examples/complex/gcd_lcm.mfl

# Build a standalone native binary
bin/machin build examples/complex/primes.mfl -o primes
./primes

# See the generated C
bin/machin build examples/demo.mfl --emit-c

# Run every example
make examples
```

Requires Go 1.22+ to build the toolchain, and a C compiler (`cc` / `gcc` / `clang`,
override with `$CC`) on PATH at compile time.

---

## For Humans

| Instead of... | In MFL... |
|---------------|-----------|
| Hand-writing source | You state intent; agents author the `.mfl` |
| Formatting & style debates | One canonical form — no choices to make |
| A REPL / interpreter | Programs compile to native binaries |
| Type annotations everywhere | Types are inferred from use |

> 💡 You author **intent**; agents write the code. The `.mfl` is plain canonical
> text, so you *can* read it — `machin build --emit-c` shows the C that runs.

## For Machines

- 🧬 **Canonical form** — one normalized function per line of plain text; functions are independently addressable, greppable, and diffable
- 🏎️ **Native codegen** — `.mfl → parse → infer types → emit C → cc -O2 → binary`
- 🧮 **Inferred static types** — `int`, `float`, `bool`, `string`, slices `[]T` — unified, no boxing
- 🧵 **Concurrency** — `go f(args)` spawns a pthread-backed goroutine; channels (`make(chan T)`, `<-`) for communication
- 🌐 **Networking** — `listen / accept / read / write / close`, the low-level shape of Go's `net`
- 🧹 **Memory** — per-goroutine arena, plus scoped `arena { }` blocks to keep a long-lived loop's memory flat
- ✅ **Compile-time type errors** — a type clash is a build failure, not a runtime surprise

```bash
machin run   <file.mfl>            # compile to native + execute
machin build <file.mfl> [-o out]   # compile to a native binary
machin build <file.mfl> --emit-c   # print the generated C
machin run|build <file.mfl> --safe # add bounds / div-zero / overflow checks
machin encode <src>                # mint canonical MFL from loose Go-like text
machin pack  <file.mfl>            # emit the dense base64 form (distribution)
```

---

## 📖 Language

Each line is one Go-flavored, deliberately minimal declaration.
See [`docs/LANGUAGE.md`](docs/LANGUAGE.md) for the full reference.

```go
func fib(n) {                       // types inferred — n is int
    if n < 2 { return n }
    return fib(n - 1) + fib(n - 2)
}

func main() {
    xs := []int{5, 3, 8}            // slices: []T over an unboxed backing array
    xs = append(xs, 1)
    total := 0
    i := 0
    for i < len(xs) {               // Go-style for (also: for {} and while cond {})
        total = total + xs[i]
        i = i + 1
    }
    println("fib(10) =", fib(10), "sum =", total)
}
```

| Feature | Detail |
|---------|--------|
| **Types** | `int` (int64), `float` (double), `bool`, `string`, slices `[]T`, structs — inferred |
| **Slices** | `[]int{...}`, `s[i]` read/assign, `len(s)`, `append(s, x)` |
| **Maps** | `make(map[K]V)`, `m[k]` read/assign, `len`, `has(m,k)`, `delete(m,k)`, `keys(m)`; int/string keys |
| **Structs** | `type T struct { ... }`, `T{f: v}` / `T{...}`, `p.f` read/assign, value semantics, `[]T` |
| **Control flow** | `if / else if / else`, `for cond {}`, `for {}`, `while cond {}`, `for k, v := range x {}` |
| **Multiple returns** | `return a, b`; `q, r := f()`; parallel `a, b = b, a`; comma-ok `v, ok := lookup(m, k)`; `_` ignores |
| **Named returns** | `func divmod(a, b) (q, r) { q = …; r = …; return }` — zero-init locals, bare `return` |
| **Variadics** | `func sum(nums...) { ... }`; call `sum(1, 2, 3)` or spread `sum(xs...)` |
| **Closures** | `func(x) { ... }` literals, capture by reference (mutable captured state), pass/return/store function values, higher-order functions |
| **Generics** | functions are implicitly generic — specialized per concrete call-site type (monomorphization), no annotations |
| **Concurrency** | `go f(args)`, channels `make(chan T)` / `ch <- v` / `<-ch`, `sleep(ms)` |
| **Operators** | `+ - * / %`, `== != < <= > >=`, `&& \|\| !`; `+` concatenates strings |
| **Builtins** | `print`, `println`, `input`, `len`, `str`, `int`, `append`, `sleep`, `has`, `delete`, `keys`, `json`, `parse`, `http_body` |
| **String ops** | `substr`, `index`, `contains`, `has_prefix`, `has_suffix`, `charat`, `to_upper`, `to_lower`, `trim`, `replace`, `split`, `join` |
| **JSON** | `json(x)` serializes any value to JSON; `parse(s, T{})` parses JSON into a value of `T` |
| **Networking** | `listen`, `accept`, `read`, `write`, `close` |
| **I/O** | `print`, `println`, `input` (read a line from stdin) |

---

## 🏗️ Architecture

```
.mfl (canonical text) ──▶ parse ──▶ infer types ──▶ emit C ──▶ cc -O2 ──▶ native binary
```

| Stage | File | What it does |
|-------|------|--------------|
| Lex | `lexer.go` | tokenizer |
| Parse | `parser.go` | precedence-climbing parser → AST (`ast.go`) |
| Type | `types.go` | inference by unification over a union-find |
| Codegen | `codegen.go` | emits standalone C99 |
| Build | `build.go` | invokes `cc -O2 -pthread` |
| CLI | `main.go` | `.mfl` loading + `run` / `build` / `encode` |

**Type inference** — every parameter, local, return, and expression gets a slot in a
union-find. Constraints merge slots; an integer literal defaults to `int` but unifies up
to `float` on contact. Slices are structural (`KSlice` + an element slot, unified
recursively), so element types infer through parameters: `func first(xs) { return xs[0] }`
works on `[]int` or `[]string` depending on the call site.

**C backend** — one C function per MFL function, with no boxing (`int64_t` / `double` /
`char*` / `mfl_slice`), so the optimizer sees ordinary C. Goroutines compile to a
per-call-site arg struct + trampoline driven by `pthread_create`.

---

## 🧩 Examples

`examples/` holds 21 programs, each a `.mfl`. `make examples` runs them all.

| Program | Shows |
|---------|-------|
| `basic/hello`, `arithmetic`, `variables`, `conditionals`, `loops`, `functions` | language tour |
| `basic/temperature` | float formulas |
| `complex/primes`, `gcd_lcm`, `collatz`, `ackermann`, `fast_power`, `isqrt` | numeric algorithms |
| `complex/to_binary`, `pi_leibniz`, `perfect_numbers` | strings, floats, divisors |
| `complex/slices` | slice literals, `append`, indexing, in-place reverse |
| `complex/maps` | word frequency + int-keyed lookup table |
| `complex/ranges` | `for k, v := range` over slices, strings, and maps |
| `complex/multi_return` | multiple returns, comma-ok, `_`, parallel swap |
| `complex/named_returns` | named return values, bare `return`, fall-through |
| `complex/variadic` | variadic params: collect, spread, fixed+variadic, generic |
| `complex/closures` | capturing lambdas, higher-order functions, IIFE |
| `complex/counter` | by-reference capture: mutable closures sharing a cell |
| `complex/arena` | scoped `arena { }`: flat memory across a long-lived loop |
| `complex/game_menu` | native desktop CLI: a Start/Settings/Exit loop reading `input()` |
| `complex/generics` | one source function specialized at int / string / float |
| `complex/goroutines` | `go` spawns concurrent workers; `sleep` waits |
| `complex/channels` | fan-in worker pool — goroutines communicate over a channel |
| `complex/http_server` | concurrent TCP/HTTP server — `go handle(conn)` per request |
| `complex/json` | `json()` serialization of scalars, slices, structs, maps |
| `complex/json_api` | JSON-over-HTTP API — returns JSON-serialized structs |
| `complex/json_parse` | `parse(s, T{})` — JSON → struct/slice/map/scalar round-trips |
| `complex/json_echo_api` | POST JSON → parse into a struct → echo it back as JSON |
| `complex/strings` | string ops: `split`/`join`/`substr`/`index`/`replace`/… + request parsing |
| `complex/router_api` | HTTP router — dispatch by method+path, extract path params |
| `bench/fib` | `fib(40)` benchmark |

```bash
# The concurrent HTTP server
machin run examples/complex/http_server.mfl
curl -i http://localhost:48080/
```

---

## ⚡ Performance

`fib(40)` — naive recursion, ~331M calls:

| Implementation | Time | Notes |
|----------------|------|-------|
| **MFL** (native, `cc -O2`) | **0.20s** | emits C, optimized by the system compiler |
| hand-written C (`cc -O2`)  | 0.19s | the baseline MFL compiles to |
| Rust (`rustc -O`)          | 0.29s | for reference |

MFL lands on hand-written C because it *is* C by the time the optimizer runs.

| Metric | Value |
|--------|-------|
| Compiled binary size (fib) | ~16 KB |
| Peak RSS (fib) | ~1.4 MB |
| Toolchain compile time | ~50 ms |

```bash
make bench        # build + time fib(40)
```

---

## 🧱 Tech Stack

| Layer | Technology |
|-------|-----------|
| Toolchain | Go 1.22 (lexer, parser, type inference, codegen) |
| Backend | C99 via `cc` / `gcc` / `clang`, `-O2 -pthread` |
| Types | Static, inferred (unification over union-find) |
| Concurrency | POSIX threads |
| Networking | BSD sockets |
| On-disk form | canonical plain text, one function per line (optional base64 via `machin pack`) |

---

## 📦 Build & Install

```bash
make build        # → bin/machin
make test         # Go test suite (compiles + runs MFL natively)
make examples     # run every example
make install      # install to $(PREFIX)/bin  (default /usr/local)
```

---

## 🌐 Status

| Capability | State |
|------------|-------|
| Canonical plain-text `.mfl` load + parse (base64 `pack` accepted) | ✅ done |
| Static type inference (no annotations) | ✅ done |
| `input()` — read a line from stdin (interactive / desktop CLI) | ✅ done |
| Native compilation via C (`run` / `build`) | ✅ done |
| `--emit-c` | ✅ done |
| int / float / bool / string | ✅ done |
| Slices `[]T` (`literal`, index, `len`, `append`) | ✅ done |
| Structs (`type T struct`, literals, field access, `[]T`) | ✅ done |
| Channels (`make(chan T)`, `ch <- v`, `<-ch`) | ✅ done |
| Maps (`make(map[K]V)`, index, `has`/`delete`/`keys`) | ✅ done |
| JSON serialization (`json(x)`) + JSON-over-HTTP example | ✅ done |
| JSON parsing (`parse(s, T{})`) + POST echo API | ✅ done |
| String ops (`split`/`join`/`substr`/`index`/…) + HTTP router | ✅ done |
| Control flow (`if`, `for`, `while`) | ✅ done |
| `range` loops over slices, maps, strings | ✅ done |
| Multiple return values + parallel/comma-ok assignment | ✅ done |
| Named return values | ✅ done |
| Variadic parameters (`f(xs...)`, spread) | ✅ done |
| Bounds / div-zero / overflow checks (`--safe`) | ✅ done |
| Closures + higher-order functions (lambda-lifting) | ✅ done |
| Generics via monomorphization (implicit, no annotations) | ✅ done |
| Arena memory management (per-goroutine; bounds servers) | ✅ done |
| Scoped arenas (`arena { }`; bounds long-lived loops) | ✅ done |
| Goroutines (`go`) + `sleep` | ✅ done |
| Networking (`listen`/`accept`/`read`/`write`/`close`) | ✅ done |
| Concurrent HTTP server example | ✅ done |
| By-reference closure capture (mutable captured state, Go semantics) | ✅ done |
| Bounds / div-zero / overflow checks (`--safe`) | ✅ done |
| Scoped arenas (`arena { }`) — bound a long-lived loop's memory | ✅ done |
| Automatic tracing GC | ⬜ planned |

---

## License

MIT — <a href="https://www.linkedin.com/in/arancibiajav/" target="_blank">Javier Leandro Arancibia</a>
