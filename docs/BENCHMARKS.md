# Benchmarks — where machin beats Rust and Zig, and where it doesn't

machin compiles MFL through C to a single native binary. That places it in the
same tier as Rust and Zig, which makes "is it actually competitive?" a fair
question — and one worth answering with numbers you can re-run rather than
adjectives.

This is the index. Every claim below links to a directory containing the source
for all three languages, a harness, and a `run.sh` that reproduces the number on
your machine.

**The short version:** machin does not beat Rust or Zig at raw speed in general —
it ties them, wins one kernel, loses another. It beats them decisively on
**binary size** and on **telling you about a bug at compile time**. It loses to
Rust on **build time** and on **default runtime safety**. All four are documented
below with equal prominence, because a benchmark suite that only lists wins is
marketing, not evidence.

## Summary

| axis | machin vs Rust | machin vs Zig | where |
|---|---|---|---|
| recursion (`fib 40`) | **26% faster** | **27% faster** | [native-speed](../bench/native-speed) |
| float loop (mandelbrot) | tie | tie (1.03×) | [native-speed](../bench/native-speed) |
| integer loop (`intsum 10⁹`) | tie (+3%) | tie (+3%) | [native-speed](../bench/native-speed) |
| array build + sieve | **1.41× slower** | **1.46× slower** | [native-speed](../bench/native-speed) |
| build time | **slower** (~100 ms vs ~57 ms) | not conclusive — see caveat | [compile-speed](../bench/compile-speed) |
| binary size floor, stripped, dynamic | **~320 kB smaller** (14 kB vs 335 kB) | — | [compile-speed](../bench/compile-speed) |
| binary size, fully static | — | **~2× larger** (940 kB vs 491 kB) | [compile-speed](../bench/compile-speed) |
| deadlock reported | **yes, Rust never** | **yes, Zig never** | [evidence](../bench/evidence) |
| out-of-range index reported at compile time | **yes, Rust never** | **yes, Zig never** | [evidence](../bench/evidence) |
| out-of-range index trapped at runtime, by default | **no — Rust wins** | tie (both need opt-in) | [evidence](../bench/evidence) |
| data race caught with no annotations | **yes** (Rust needs `Send`/`Sync`, `Arc`/`Atomic`) | Zig has no analysis | [race-freedom](../bench/race-freedom) |

Re-measured 2026-08-07: `native-speed`, `compile-speed`, `evidence`. The
`rest-sqlite`, `cold-start` and `tls-static` numbers are as published in their own
READMEs and were not re-run in that pass.

## The three things machin genuinely wins

### 1. It reports bugs the other two never mention

This is the real differentiator, and it is not a speed claim.

A **guaranteed deadlock** — a receive on a channel nothing ever sends to —
is reported by `machin check` as `DL001` at compile time, and if one survives to
runtime the process exits 2 with a causal wait-cycle instead of hanging. The same
program in Rust (`rx.recv()` with the sender alive) and Zig (`sem_wait` nobody
posts) compiles clean in both and **hangs forever**.

Rust's type system prevents *data races*. It has never claimed to prevent
*deadlocks*, and a `recv()` that can never return is idiomatic, `unsafe`-free,
well-typed Rust.

An **out-of-range index** is reported by `machin falsify` as `FALS001` with a
concrete failing input, before the program runs, on code whose only call site is
in range. `rustc` and `zig` say nothing.

**Neither analysis proves absence, and neither should be read that way.**
`falsify` is unsound-complete: every counterexample it reports is real, but a
clean result means "no bug within the bounds", never "correct". `DL001` is the
opposite trade — sound and false-positive-free, so it only fires when it can prove
a channel is never fed, which means a clean result is not a proof of
deadlock-freedom either (the runtime detector catches the rest). "machin found
nothing" is weaker than "machin found this bug, here is the input".

→ [bench/evidence](../bench/evidence)

### 2. Binary size

Stripped, with both dynamically linked against the system libc — a like-for-like
comparison — machin produces **14 kB** where Rust produces **335 kB**. There is
no std runtime to link.

**Read that as a fixed offset, not a ratio.** machin's hello world and its fib(40)
are *byte-identical* in size (14,544 B), and Rust's differ by 2% (343,568 vs
335,472). Neither is measuring the program — both measure the baseline each
toolchain links in. Real code adds real bytes to both; machin's JSON+HTTP example
is ~12 kB above its own floor. The durable claim is **"Rust starts ~320 kB
ahead"**, not "machin binaries are 24× smaller"; the ratio shrinks as programs
grow, the offset does not. For a real service rather than a kernel, see
[cold-start](../bench/cold-start).

And fully static, Zig wins: 491 kB against machin's 940 kB.

→ [bench/compile-speed](../bench/compile-speed)

### 3. Data-race freedom with zero annotations

`machin check` infers whether goroutines can race, with no `Send`/`Sync`, no
`Arc`, no `Mutex` — and `--race-safe` refuses the build. Rust reaches the same
safety, but by either rejecting the natural shape of the program or requiring
wrapper types. Go compiles the race silently.

→ [bench/race-freedom](../bench/race-freedom)

## The things machin loses, stated plainly

### Build time — Rust is faster

Bare `rustc -C opt-level=3` builds these single-file kernels in ~57 ms; machin
takes ~87–122 ms, because its number includes the `cc -O2` backend run. Rust wins
this outright, and `cargo` was deliberately not used so the comparison could not
be inflated in machin's favour.

Zig's column is **not reported as a result**. On the development machine every
`-OReleaseFast` build touching `std.debug.print` costs ~13 s and is not reused
between identical invocations, while the same program in Debug costs 0.5 s and one
that prints nothing costs 0.22 s. That is the cost of compiling std's formatting
under ReleaseFast on a 0.16 beta — it says nothing about how Zig scales with your
code, so no conclusion is drawn from it.

### Default runtime safety — Rust is safer

machin's **default** build omits bounds checks, exactly like Zig's `ReleaseFast`.
Given an out-of-range index, machin printed a silent wrong `0` and exited
successfully; Zig read `281479271677952` out of adjacent memory and exited
successfully. Rust trapped (exit 101). Both machin and Zig need an opt-in
(`--safe`, `ReleaseSafe`) to get there.

So the honest claim is *"machin tells you earlier"*, **not** *"machin is safer
than Rust"*. Earlier is worth a lot — a bug caught at build time with a concrete
input costs minutes, and the same bug reaching production as a hung process costs
far more — but it is a different claim, and it does not replace Rust's.

### The sieve — 1.46× slower, and the published reason was wrong

The benchmark long explained this as machin's "slice indexing/layout" being worse
than a Rust `Vec`. Phase-timing disproves that:

| phase | machin | Rust |
|---|--:|--:|
| build the 10M array by `append` | 70–83 ms | 27–29 ms |
| **the sieve loop itself** | **110 ms** | **111 ms** |
| the count loop | 5–7 ms | 3 ms |

Indexing ties exactly. The whole gap is `append`'s growth path: an arena frees
nothing mid-life, so `mfl_realloc` can only ever allocate fresh and memcpy, never
extend in place the way `Vec::push` does via `mremap`.

The obvious fix — hand the arena's newest block straight to `realloc` — was
implemented, tested, and **rejected as unsound**: MFL slices share backing storage
(`b := a` aliases, and so does passing a slice to a function), so in-place growth
would turn every existing alias into a use-after-free. Tracked with sound
alternatives in [#578](https://github.com/javimosch/machin/issues/578).

## Methodology

Benchmarks are easy to make lie. What this suite does about it:

- **Identical output is verified**, byte-for-byte, across all three languages on
  every run. If the programs disagree, the timing is meaningless and the harness
  says so.
- **Timing is interleaved and rotated**, not run language-by-language. An earlier
  version ran all N machin samples, then all N Rust, then all N Zig — on a laptop
  that heats up during a multi-second kernel, that penalizes whoever runs last.
  Fixing it changed a published claim: the integer loop's "20–25% win" became a
  3% tie.
- **Differences under 3% are reported as ties.** Worst observed run-to-run spread
  on this laptop was 41% of the min sample; declaring winners inside that is how
  benchmarks start lying.
- **Each language at its real release setting** — Rust `opt-level=3` (not the
  weaker `-O`), Zig `ReleaseFast`, machin `cc -O2`. No `-march=native` for anyone.
- **Sizes are reported stripped and with linkage**, because "as-produced" mostly
  measures how much debug info each toolchain leaves behind, and comparing a
  dynamic binary to a static one is not like-for-like.
- **Exit codes are captured from the program, never from a pipeline stage.** An
  earlier harness used `cmd | sed; echo $?`, which reports `sed`'s status — it
  silently recorded failed Zig builds as successes.
- **Absolute milliseconds are machine-specific.** The ratios are the portable
  result; re-run everything on your own hardware.

## All benchmarks

| directory | question |
|---|---|
| [native-speed](../bench/native-speed) | How fast does the compiled program run vs Rust and Zig? |
| [compile-speed](../bench/compile-speed) | How long until there's a binary, and how big is it? |
| [evidence](../bench/evidence) | Which toolchain tells you about the bug, and when? |
| [race-freedom](../bench/race-freedom) | Who catches a data race, and what does the fix cost? |
| [rest-sqlite](../bench/rest-sqlite) | How many tokens does an agent spend to write a real service? |
| [cold-start](../bench/cold-start) | Ship size, cold-start latency, resident memory vs Node. |
| [tls-static](../bench/tls-static) | What does a TLS-calling app cost, fully static, `FROM scratch`? |
