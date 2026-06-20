# Benchmarks

Reproducible performance comparison for `fib(40)` — naive recursion, ~331M calls.

The numbers in the top-level `README.md` "Performance" section are **illustrative**
and hardware-dependent. This directory lets you regenerate them on your own machine
instead of taking them on faith.

## Files

| File            | Role                                                       |
|-----------------|------------------------------------------------------------|
| `fib.mfl`       | the MFL program under test (base64, one function per line) |
| `fib.c`         | hand-written C baseline — the algorithm MFL compiles to    |
| `fib.rs`        | Rust reference implementation                              |
| `run_bench.sh`  | builds all three natively, times them, prints a table      |

## Running

```bash
# from the repo root
make bench-report          # builds toolchain + runs the harness

# or directly, with a custom repetition count (best run is kept)
examples/bench/run_bench.sh 7
```

Requirements: `go` and a C compiler (`cc`/`gcc`/`clang`). `rustc` is optional —
the Rust row is skipped automatically if it is not on `PATH`.

## What it does

1. Builds the `machin` toolchain from source.
2. Compiles `fib.mfl` to a native binary via MFL's C backend (`cc -O2`),
   `fib.c` with `cc -O2`, and `fib.rs` with `rustc -O`.
3. Verifies all three print the same answer (`fib(40) = 102334155`) before
   trusting any timing.
4. Times each binary `RUNS` times and reports the best wall-clock per row.

Because MFL *is* C by the time the optimizer runs, the MFL and hand-written C
rows should land in the same class; absolute values vary by CPU and compiler.

`make bench` remains a lighter target that just builds and `time`s the MFL binary.
