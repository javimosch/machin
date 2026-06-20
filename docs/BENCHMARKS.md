# Benchmarks

This document explains how MFL's performance numbers are produced and how to
reproduce them. The headline claim is simple: because MFL compiles to C and
hands it to `cc -O2`, scalar workloads land on the same machine-code class as
hand-written C.

## Reproducing

```bash
make bench-report          # or: bash scripts/bench.sh [N]
```

`scripts/bench.sh`:

1. builds the MFL compiler with `go build`,
2. compiles `examples/bench/fib.mfl` to a native binary via `machin build`
   (which emits C and invokes `cc -O2`),
3. generates an **equivalent** hand-written C source and compiles it with
   `cc -O2`,
4. generates an **equivalent** Rust source and compiles it with `rustc -O`
   (skipped if `rustc` is not installed),
5. times each binary best-of-3 (wall clock) and prints a Markdown table.

The C and Rust sources are generated inside the script, so the comparison is
self-contained — no checked-in binaries, no hidden flags.

## Workload

`fib(40)` — naive double recursion, ~331M calls. This is a deliberately
branch- and call-heavy integer microbenchmark: it stresses the function-call
ABI and the optimizer's ability to keep recursion cheap, with no allocation,
I/O, or floating point to muddy the comparison.

```go
func fib(n) { if n < 2 { return n } return fib(n - 1) + fib(n - 2) }
func main() { println(fib(40)) }
```

## Sample result

On an `x86_64` Linux box with `cc (Ubuntu 13.3.0) 13.3.0` and
`rustc -O`, best-of-3 wall-clock:

| Implementation            | Time    | Notes                                     |
|---------------------------|---------|-------------------------------------------|
| **MFL** (native, cc -O2)  | ~0.15s  | emits C, optimized by the system compiler |
| hand-written C (cc -O2)   | ~0.13s  | the baseline MFL compiles to              |
| Rust (rustc -O)           | ~0.24s  | for reference                             |

MFL binary size: ~16 KB.

**Absolute times vary by machine, compiler, and load — the ratios are the
point.** MFL tracks hand-written C closely (it *is* C by the time the optimizer
runs); the small residual gap comes from MFL using `long`-width integers and a
fixed prologue, not from interpretation overhead.

## Caveats

- Best-of-3 reduces noise but is not a statistical benchmark; for rigorous
  numbers use `hyperfine` or `perf stat` on the generated binaries.
- The Rust column exists only as a familiar reference point; the codegen
  strategies differ (LLVM vs. the system `cc`), so do not read it as a language
  ranking.
- Results depend on your `cc` (`gcc` vs `clang`) and its version. Set `CC=clang`
  to compare backends: `CC=clang make bench-report`.
