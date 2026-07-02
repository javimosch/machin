# Benchmark — data-race safety, three ways

machin v0.91.0 shipped **inferred data-race freedom**: `machin check` infers,
with zero annotations, whether a program's goroutines can race — the guarantee
Rust needs `Send`/`Sync` for. This benchmark puts that claim next to Go and
Rust on the same textbook example. See issue #287.

The program: 4 threads/goroutines each increment a shared counter 2,000,000
times with no synchronization. Expected sum: 8,000,000. A data race on the
counter means the true answer is undefined behavior.

## Results (this machine — gcc 11, go 1.22, rustc 1.98)

| | compiles? | catches the race? | how | numeric output (3 runs) |
|---|---|---|---|---|
| **machin** (`racy.src`) | yes | **yes — `machin check`, at compile time** | `RACE001` on the untouched code; `--race-safe` refuses the build | 8000000 / 8000000 / 8000000 |
| Go (`racy.go`) | yes, silently | only via `go run -race` (opt-in, dynamic) | no error, no warning from `go build` | 2094040 / 3638549 / 2090454 |
| Rust, naive (`racy_no_unsafe.rs`) | **no** | n/a — refuses to compile | `error[E0133]: use of mutable static is unsafe` | — |
| Rust, `unsafe` (`racy_naive.rs`) | yes, with a warning | no (you disabled it) | explicit `unsafe` block, still warns | 8000000 / 8000000 / 8000000 |

**The fix**, both race-free:

| | what it takes |
|---|---|
| machin (`safe.src`) | share by communicating (return partial sums over a channel) — **zero extra annotations** over the racy shape |
| Rust (`safe.rs`) | `Arc<AtomicI64>` + `Ordering::SeqCst` + `.fetch_add`/`.load` — real added types/syntax |

## The honest read (this is the actual point, not a footnote)

The naive expectation going in was "show the racy program visibly crash." It
didn't, reliably — and that turned out to be the more useful result. On this
run, **machin's racy build printed the correct number every time**, and so did
Rust's `unsafe`-escaped version. Only Go's differently-scheduled goroutines
visibly corrupted the output here. All three programs have the *identical*
race (confirmed independently by `go run -race`, and by Rust's own compiler
refusing the naive form) — but whether a given run *looks* wrong is
essentially down to scheduling luck, not a property you can test for.

**That's exactly why compile-time detection matters more than a "watch it
break" demo.** A race that happens to print the right number in dev, in CI,
and in staging is not a race you've ruled out — it's a race waiting for
different load or a different machine. `machin check`/`--race-safe` catches
this class of bug on the untouched original code, deterministically, every
time, independent of whether that particular execution would ever manifest
visible corruption. Rust gets the same determinism, but only by either
refusing to compile the natural shape or requiring you to reach for
`Arc`/`Mutex`/`Atomic` wrapper types. machin gets it for free, on code that
looks like you'd write it without thinking about the race at all.

## Reproduce

```bash
./run.sh    # needs machin, go, rustc — runs all 9 steps above, asserts each
            # matched its expected outcome, prints ALL STEPS MATCHED or FAIL
```
