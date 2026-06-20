# Benchmarks

MFL's headline claim is that it compiles to native code competitive with
hand-written C. This page makes that claim **reproducible**: the numbers below
are produced by a script in the repo, not typed in by hand.

## Running it yourself

```bash
make bench-report          # build everything + print the table
# or, directly:
examples/bench/run.sh      # human-readable report
RUNS=10 examples/bench/run.sh   # more repetitions, tighter best-of
```

The harness ([`examples/bench/run.sh`](../examples/bench/run.sh)):

1. Builds the MFL toolchain from source and compiles
   [`examples/bench/fib.mfl`](../examples/bench/fib.mfl) to a native binary.
2. Compiles the hand-written C baseline
   ([`fib.c`](../examples/bench/fib.c)) with `cc -O2`.
3. Compiles the Rust reference
   ([`fib.rs`](../examples/bench/fib.rs)) with `rustc -O`.
4. **Verifies every implementation prints the same result** (`fib(40) ==
   102334155`) — a benchmark that computes the wrong answer is meaningless.
5. Runs each binary `RUNS` times (default 5) and keeps the best wall-clock
   time, then prints a Markdown table.

C and Rust are compiled only if `cc` / `rustc` are on your `PATH`; missing
toolchains are skipped with a note rather than failing the run, so the MFL
number is always reproducible even on a bare machine.

## The workload

`fib(40)` via naive double recursion — ~331M calls, no memoization. It is a
pure compute / call-overhead microbenchmark: it stresses the quality of the
emitted code and the call ABI, nothing else. All three implementations use the
identical algorithm and a 64-bit integer type.

## A sample run

Measured on the reference CI host — your absolute numbers will differ, but the
*ratios* are the point. Re-run the script to get figures for your machine.

| Implementation | Time | Notes |
|----------------|------|-------|
| **MFL** (native, `cc -O2`) | **0.155s** | emits C, optimized by the system compiler |
| hand-written C (`cc -O2`)  | 0.125s | the baseline MFL compiles to |
| Rust (`rustc -O`)          | 0.242s | for reference |

Toolchain versions for the run above:

```
cc    (Ubuntu 13.3.0) gcc 13.3.0
rustc 1.75.0
go    1.22
```

MFL lands within striking distance of hand-written C because, by the time the
optimizer runs, it *is* C. The small remaining gap is codegen overhead (extra
temporaries, a thinner set of `__attribute__` hints) that the C compiler can't
always see through; it is not algorithmic.

> **Note on methodology.** These are best-of-N wall-clock times on a shared
> machine, not a rigorously isolated benchmark. They are meant to substantiate
> the "compiles to C-class native code" claim, not to rank compilers to the
> millisecond. For your own hardware, run the script.
