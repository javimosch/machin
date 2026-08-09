# Benchmark — build time and binary size vs Rust & Zig

[`../native-speed`](../native-speed) measures how fast the compiled program runs.
This measures the two things you deal with *before* that: how long you wait for a
binary, and how big it is when you get one.

Same four kernels, byte-identical output, each language at its real release
setting.

## Build time — machin loses to Rust, beats Zig

Source → runnable optimized native binary, min of 3 builds, Zig 0.15.2:

| kernel | machin | Rust `-O3` | Zig cold | Zig warm |
|---|--:|--:|--:|--:|
| fib | 96 ms | **52 ms** | 3507 ms | 297 ms |
| mandel | 90 ms | **58 ms** | 3517 ms | 289 ms |
| sieve | 99 ms | **75 ms** | 3477 ms | 354 ms |
| intsum | 103 ms | **51 ms** | 3535 ms | 287 ms |

**Rust wins this outright**, roughly 1.5×. machin's number includes its `cc -O2`
backend run — the whole pipeline to a runnable binary, which is the honest thing to
measure even though it's the unflattering one. Rust is invoked as bare `rustc` on a
single file, **not** `cargo build`: cargo would add manifest parsing and lockfile
resolution that has nothing to do with compiler speed, and charging that to Rust
would have made machin look good for a bad reason.

machin beats Zig ~3× warm and ~35× cold.

### Which Zig, and why it matters a lot here

An earlier version of this benchmark reported Zig at **~13 s** and declined to draw
any conclusion, blaming a "0.16.0 beta from snap". **Both halves of that were
wrong**, and the correction changes the result:

- 0.16.0 is not a beta. It is the current **stable** release (2026-04-13). The snap
  channel name (`latest/beta`) misled us.
- It is not a packaging artifact. The official ziglang.org binaries reproduce it
  exactly.

What is actually happening is a **caching regression between 0.15.2 and 0.16.0**,
on the same program and the same machine:

| | 1st build | 2nd, identical | 
|---|--:|--:|
| Zig 0.15.2 | 3.2 s | **0.28 s** |
| Zig 0.16.0 | 16.1 s | **13.3 s** |

0.15.2 reuses its cache; 0.16.0 effectively does not — a ~45× warm-build
regression. It reproduces with an explicit writable `--global-cache-dir`, which
fills to 45 MB and still doesn't help.

So this benchmark reports **0.15.2**, where Zig's caching works as intended, and
you can point it at any toolchain:

```bash
ZIG=~/opt/zig-x86_64-linux-0.15.2/zig ./run.sh
```

Reporting 0.16.0 instead would credit machin with a ~130× build-time win that is
really someone else's caching bug. The whole cost, in both versions, is compiling
std's formatting under `ReleaseFast` — a program that prints nothing builds in
0.22 s.

## Binary size — machin wins big, dynamically

As produced / stripped:

| kernel | machin | Rust | Zig |
|---|--:|--:|--:|
| fib | 17 kB / **14 kB** `[dyn]` | 4291 kB / 335 kB `[dyn]` | 874 kB / **16 kB** `[static]` |
| mandel | 17 kB / **14 kB** `[dyn]` | 4291 kB / 336 kB `[dyn]` | 874 kB / **16 kB** `[static]` |
| sieve | 17 kB / **14 kB** `[dyn]` | 4292 kB / 336 kB `[dyn]` | 874 kB / **16 kB** `[static]` |
| intsum | 17 kB / **14 kB** `[dyn]` | 4291 kB / 335 kB `[dyn]` | 874 kB / **16 kB** `[static]` |

**Compare like with like.** machin and Rust both link dynamically against the
system libc, so their stripped numbers are directly comparable: **14 kB vs
335 kB — about 24×.** There is no std runtime to link; machin's output is C, and
C's runtime is already on the machine.

### What that 24× is, and is not

It is a comparison of each toolchain's **fixed floor**, not evidence that machin
binaries scale 24× better. These kernels are tiny, and the numbers say so plainly:

| program | machin | Rust |
|---|--:|--:|
| hello world | 14,544 B | 343,568 B |
| fib(40) | 14,544 B | 335,472 B |
| a JSON+HTTP example | 26,840 B | — |

machin's hello world and its fib are **byte-identical in size**, and Rust's differ
by 2%. Neither number is measuring the program; both are measuring the baseline
each toolchain links in. Real code adds real bytes to both — machin's JSON+HTTP
example is ~12 kB above its own floor.

So the honest form of the claim is **"Rust starts about 320 kB ahead"**, not
"machin binaries are 24× smaller". The ratio narrows as programs grow; the
constant offset is what persists. That offset is what matters if you ship
containers or care about cold start — see [`../cold-start`](../cold-start), which
measures a real REST+SQLite service rather than a kernel.

The as-produced column (17 kB vs 4291 kB, ~250×) is a much bigger number and a
much worse comparison — it mostly measures how much debug info each toolchain
leaves in by default. It's shown for completeness, not as the headline.

Zig links statically here, so it carries libc the other two borrow — and it still
comes out at **16 kB**, about the size of machin's *dynamic* binary while needing
nothing on the target at all.

**This corrects an earlier version of this file**, which reported Zig at 491 kB
static and concluded "fully static, Zig wins" by a mere 2x. That 491 kB was
measured on Zig **0.16.0**, which inflates it ~32x — the same release whose build
cache regressed. On 0.15.2:

```
zig 0.15.2    873,760 B as-produced ->   15,760 B stripped  [static]
zig 0.16.0  3,594,056 B as-produced ->  503,256 B stripped  [static]
machin                                   14,544 B stripped  [dynamic]
machin --static                         940,000 B           [static]
```

So the honest ranking on size is:

- **vs Rust, machin wins** — 14 kB against 335 kB, both dynamic. That holds
  everywhere it has been measured.
- **vs Zig, machin does not win.** Zig 0.15.2's *fully static* 16 kB is
  effectively the same size as machin's *dynamic* 14 kB, and **~60x smaller than
  machin's own static build** (940 kB, which bundles SQLite). For shipping a
  self-contained artifact — a container, a copied binary — Zig is simply better
  at this than machin is.

## The honest read

Two clear results in opposite directions:

- **Binary size is a genuine machin win**, but it is a *fixed-floor* win: Rust
  starts ~320 kB ahead at equal linkage, and that offset persists rather than the
  24× ratio (see above — machin's hello world and its fib are the same size to the
  byte). If you ship containers or care about cold start, the offset is the number
  that matters (see also [`../cold-start`](../cold-start)).
- **Build time is a genuine machin loss.** Rust is ~1.5× faster at producing these
  binaries and machin should not pretend otherwise. machin's compensation is
  `machin check` — lex/parse/typecheck/race/arena analysis with no `cc` run at all,
  in milliseconds — which is the loop an agent actually spends its time in. That's
  a different operation from a full build, so it isn't in this table; comparing it
  to `rustc`'s full build would be the same kind of cheat as charging Rust for
  cargo.

## Reproduce

```bash
./run.sh
```

Needs `machin`, `rustc`, `zig`, `python3`, and `strip`.
