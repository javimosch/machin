<p align="center">
  <img src="https://img.shields.io/badge/version-0.1.0-blue" alt="Version">
  <img src="https://img.shields.io/badge/license-MIT-green" alt="License">
  <img src="https://img.shields.io/badge/go-1.22-00ADD8" alt="Go">
  <img src="https://img.shields.io/badge/backend-C%20%E2%86%92%20native-orange" alt="Native">
</p>

<h1 align="center">machin ⎯ Machine-First Language</h1>

<p align="center">
  <b>A backend language based on Go — but the source is base64.</b><br>
  <b>Written for machines, not humans.</b>
</p>

> Think: "Go, but each function is one line of base64, and it compiles to native code at C speed."

## ⚡ TL;DR

> A statically-typed language whose on-disk form is base64 — one function per line, blank line between functions. It compiles to native machine code through C.

```bash
# A program IS base64 — one function per line:
cat examples/demo.mfl
#   ZnVuYyBmaWIobikgeyBpZiBuIDwgMiB7IHJldHVybiBuIH0g...
#
#   ZnVuYyBtYWluKCkgeyBwcmludGxuKGZpYigxMCkpIH0=

# Compile to native + run
machin run examples/demo.mfl

# Compile to a standalone native binary
machin build examples/complex/primes.mfl -o primes && ./primes

# Inspect the C the compiler emits
machin build examples/demo.mfl --emit-c
```

👉 The `.mfl` base64 **is** the program — there is no human-readable source of truth
👉 Statically typed by inference — no annotations
👉 Compiles to native code via `cc -O2` — fib(40) runs neck-and-neck with hand-written C

## The Problem

Programming languages are designed around human ergonomics: readable keywords, whitespace, comments, syntax highlighting. But increasingly the entity reading and writing code is a machine, and that surface is just overhead:

- **Glyphs are for eyes** — indentation, formatting, and naming conventions exist so humans can scan code
- **"Readable source" implies a human in the authoring loop** — but the machine can emit and consume a denser form directly
- **Interpreted or VM languages trade speed for convenience** — convenience the machine doesn't need

## The Solution

machin (the toolchain) compiles **MFL** (the language):

- **Machine-first** — a program is base64, one function per line, a blank line between functions. The human states intent; the machine reads and writes the code. There is no `decode` step in the workflow.
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
| Reading source code | You don't — the machine reads the base64 |
| Authoring readable text | You state intent; the machine emits `.mfl` |
| A REPL / interpreter | Programs compile to native binaries |
| Type annotations everywhere | Types are inferred from use |

> 💡 You author **intent**. The `.mfl` is the machine's artifact. If you ever need to
> look, `machin build --emit-c` shows exactly what runs.

## For Machines

- 🧬 **Dense canonical form** — base64, one function per line; functions are independently addressable
- 🏎️ **Native codegen** — `.mfl → parse → infer types → emit C → cc -O2 → binary`
- 🧮 **Inferred static types** — `int`, `float`, `bool`, `string`, slices `[]T` — unified, no boxing
- 🧵 **Concurrency** — `go f(args)` spawns a pthread-backed goroutine
- 🌐 **Networking** — `listen / accept / read / write / close`, the low-level shape of Go's `net`
- ✅ **Compile-time type errors** — a type clash is a build failure, not a runtime surprise

```bash
machin run   <file.mfl>            # compile to native + execute
machin build <file.mfl> [-o out]   # compile to a native binary
machin build <file.mfl> --emit-c   # print the generated C
machin encode <src>                # machine tool: mint MFL from loose Go-like text
```

---

## 📖 Language

The decoded form of each base64 line is Go-flavored and deliberately minimal:

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
| **Types** | `int` (int64), `float` (double), `bool`, `string`, slices `[]T` — inferred |
| **Slices** | `[]int{...}`, `s[i]` read/assign, `len(s)`, `append(s, x)` |
| **Control flow** | `if / else if / else`, `for cond {}`, `for {}`, `while cond {}` |
| **Concurrency** | `go f(args)`, `sleep(ms)` |
| **Operators** | `+ - * / %`, `== != < <= > >=`, `&& \|\| !`; `+` concatenates strings |
| **Builtins** | `print`, `println`, `len`, `str`, `int`, `append`, `sleep` |
| **Networking** | `listen`, `accept`, `read`, `write`, `close` |

---

## 🏗️ Architecture

```
.mfl ──base64 decode──▶ parse ──▶ infer types ──▶ emit C ──▶ cc -O2 ──▶ native binary
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

`examples/` holds a growing set of programs, each a `.mfl`. `make examples` runs them all.

| Program | Shows |
|---------|-------|
| `basic/hello`, `arithmetic`, `variables`, `conditionals`, `loops`, `functions` | language tour |
| `basic/temperature` | float formulas |
| `complex/primes`, `gcd_lcm`, `collatz`, `ackermann`, `fast_power`, `isqrt` | numeric algorithms |
| `complex/armstrong`, `nth_prime`, `power_table` | digit math, prime search, integer powers |
| `complex/to_binary`, `pi_leibniz`, `perfect_numbers` | strings, floats, divisors |
| `complex/slices` | slice literals, `append`, indexing, in-place reverse |
| `complex/goroutines` | `go` spawns concurrent workers; `sleep` waits |
| `complex/http_server` | concurrent TCP/HTTP server — `go handle(conn)` per request |
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

These numbers are **machine-dependent**. Reproduce them on your own box with
`make bench-report`, which builds all three contenders from
[`examples/bench/`](examples/bench/), times them, and regenerates
[`docs/BENCHMARKS.md`](docs/BENCHMARKS.md).

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
| On-disk form | base64, one function per line |

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
| base64 `.mfl` load + parse | ✅ done |
| Static type inference (no annotations) | ✅ done |
| Native compilation via C (`run` / `build`) | ✅ done |
| `--emit-c` | ✅ done |
| int / float / bool / string | ✅ done |
| Slices `[]T` (`literal`, index, `len`, `append`) | ✅ done |
| Control flow (`if`, `for`, `while`) | ✅ done |
| Goroutines (`go`) + `sleep` | ✅ done |
| Networking (`listen`/`accept`/`read`/`write`/`close`) | ✅ done |
| Concurrent HTTP server example | ✅ done |
| Maps, structs, channels | ⬜ planned |
| Bounds / overflow checks (`--safe`) | ⬜ planned |

---

## License

MIT — <a href="https://www.linkedin.com/in/arancibiajav/" target="_blank">Javier Leandro Arancibia</a>
