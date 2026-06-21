# Changelog

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
