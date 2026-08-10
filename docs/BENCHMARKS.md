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
| array build + sieve | **tie** (1.01×, was 1.41×) | 1.19× slower (was 1.46×) | [native-speed](../bench/native-speed) |
| build time | **slower** (~100 ms vs ~57 ms) | **~3x faster** warm, ~35x cold (Zig 0.15.2) | [compile-speed](../bench/compile-speed) |
| binary size floor, stripped, dynamic | **~320 kB smaller** (14 kB vs 335 kB) | tie (Zig 16 kB, and static) | [compile-speed](../bench/compile-speed) |
| binary size, fully static | — | **~60× larger** (940 kB vs 16 kB) | [compile-speed](../bench/compile-speed) |
| deadlock reported | **yes, Rust never** | **yes, Zig never** | [evidence](../bench/evidence) |
| out-of-range index reported at compile time | **yes, Rust never** | **yes, Zig never** | [evidence](../bench/evidence) |
| out-of-range index trapped at runtime, by default | **no — Rust wins** | tie (both need opt-in) | [evidence](../bench/evidence) |
| authoring cost, REST+SQLite service | **1.87x cheaper** (388 vs 727 tokens, 0 deps vs 37 crates) | not comparable — no SQLite in std | [rest-sqlite](../bench/rest-sqlite) |
| authoring cost, generic algorithmic code | **1.16x cheaper** (after #580) | — | [rest-sqlite](../bench/rest-sqlite) |
| data race caught with no annotations | **yes** (Rust needs `Send`/`Sync`, `Arc`/`Atomic`) | Zig has no analysis | [race-freedom](../bench/race-freedom) |

Re-measured 2026-08-07: `native-speed`, `compile-speed`, `evidence`. The
`rest-sqlite`, `cold-start` and `tls-static` numbers are as published in their own
READMEs and were not re-run in that pass.

## The four things machin genuinely wins

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

**Against Zig, machin does not win this.** An earlier version of this document
said "fully static, Zig wins" by about 2× — 491 kB against machin's 940 kB. That
491 kB was measured on Zig **0.16.0**, which inflates it ~32×. On 0.15.2, Zig's
*fully static* stripped binary is **16 kB** — effectively the same size as
machin's *dynamic* one, while needing nothing on the target — and **~60× smaller
than machin's own `--static` build**. For shipping a self-contained artifact, Zig
is simply better at this. The size win is over Rust, not over Zig.

→ [bench/compile-speed](../bench/compile-speed)

### 3. Authoring cost, where the batteries are

The same notes REST service over SQLite costs **388 tokens in machin and 727 in
Rust** — 87% more — and Rust needs 37 transitive crates and a 91 MB `target/`
directory to get there, against machin's zero dependencies and a 49 kB binary.

This is narrower than it first looks, and the narrowing is the useful part. On
*generic algorithmic* code Rust's stdlib is rich and the two are close — machin
only pulled ahead there once `sort`/`sort_by` shipped (#580), and by 16%, not 87%.
The large win is specifically where machin has batteries: HTTP, SQLite, JSON and
the router are in the box.

So the claim is **"machin is terser than Rust exactly where machin has
batteries"**, not "machin is terser than Rust".

→ [bench/rest-sqlite](../bench/rest-sqlite)

### 4. Data-race freedom with zero annotations

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

Against Zig, machin wins: ~3x faster than a warm Zig build and ~35x faster than a
cold one (Zig 0.15.2 — 290 ms warm, 3.5 s cold).

**That number depends on which Zig, and an earlier version of this document got it
wrong.** It reported Zig at ~13 s and drew no conclusion, blaming "a 0.16.0 beta
from snap". 0.16.0 is not a beta — it is the current stable release — and the
official ziglang.org binaries reproduce the 13 s exactly, so it was not a packaging
artifact either. What it actually is: a **caching regression between 0.15.2 and
0.16.0**. 0.15.2 rebuilds an unchanged program in 0.28 s; 0.16.0 takes 13.3 s, ~45x
slower. The benchmark therefore reports 0.15.2, where caching works. Quoting 0.16.0
would credit machin with a ~130x win that is really someone else's bug.

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

### The sieve — fixed, and the published reason had been wrong

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

The obvious fix — hand the arena's newest block straight to `realloc` — is
**unsound**: MFL slices share backing storage (`b := a` aliases, and so does
passing a slice to a function), so unconditional in-place growth turns every
existing alias into a use-after-free.

**#578 fixed it properly.** `machin alias` proves, fail-closed, that a local slice
has no other live reference across an append; only then does codegen grow the
block in place. An adversarial suite of live-alias shapes runs under
AddressSanitizer in CI, and was verified to catch the unsound version.

Measured on the CI runner before and after, normalized against Rust because the
runner drifted ~15% between sessions: **machin/Rust went 1.10× → 1.01×** on the
sieve. It now ties Rust. It still trails **Zig** by ~1.19× (down from 1.32×) —
a real remaining gap, reported as one.

## Verified on a second machine

Every number here was originally measured on one laptop with a worst observed
run-to-run spread of **41%**, and this document told you that "absolute
milliseconds are machine-specific; the **ratios** are the portable result." That
was an assertion, not a finding — so it was tested.

`.github/workflows/bench.yml` runs the suite on a GitHub runner (manual dispatch)
and writes the tables into both the log and the job summary, so the second machine
is one nobody involved controls and anyone can re-check the output.

**Runner:** AMD EPYC 7763, 4 cores, load 0.85, gcc 13, rustc 1.97.1, zig 0.15.2 —
a different CPU vendor, different compilers, and much quieter (15% spread vs 41%).

| claim | laptop | EPYC runner | holds? |
|---|---|---|---|
| fib: machin faster | 26% | 28% | **yes** |
| mandelbrot | near-tie (1.03×) | tie | **yes** |
| intsum | tie (+3%) | tie | **yes** |
| sieve: machin slower | 1.46× | **1.32×** | direction yes, magnitude drifts |
| Rust wins build time | yes | yes | **yes** |
| machin ~3× faster than a warm Zig build | yes | yes | **yes** |
| machin 14 kB vs Rust ~335 kB | yes | 14 kB vs 334 kB | **yes, exactly** |
| every `evidence` verdict | — | identical | **yes** |

**The verdicts are portable; the exact ratios are not always.** Three of the four
runtime verdicts reproduce unchanged, and the deadlock/out-of-range results are
bit-for-bit the same story (even Zig's out-of-bounds garbage differs between
machines, which is what undefined behaviour should do). The sieve gap, though,
moved from 1.46× to 1.32× — a ~10% swing. That is consistent with its cause: the
gap is `append` faulting in fresh pages (#578), which is the most
memory-subsystem-sensitive thing in the suite, and the EPYC has a better one.

So read a **verdict** ("machin wins recursion, loses the sieve") as portable, and a
**precise ratio** as this-machine-specific. Where this document quotes a ratio, it
is the laptop's.

The run also **caught a wrong published claim** — the Zig static binary size above
— which is the entire reason for doing this.

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
