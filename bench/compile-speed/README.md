# Benchmark — build time and binary size vs Rust & Zig

[`../native-speed`](../native-speed) measures how fast the compiled program runs.
This measures the two things you deal with *before* that: how long you wait for a
binary, and how big it is when you get one.

Same four kernels, byte-identical output, each language at its real release
setting.

## Build time — machin loses to Rust

Source → runnable optimized native binary, min of 3 builds:

| kernel | machin | Rust `-O3` | Zig `ReleaseFast` |
|---|--:|--:|--:|
| fib | 95 ms | **52 ms** | 11418 ms † |
| mandel | 87 ms | **54 ms** | 12589 ms † |
| sieve | 96 ms | **74 ms** | 13813 ms † |
| intsum | 116 ms | **73 ms** | 12863 ms † |

**Rust wins this outright**, roughly 1.5×. machin's number includes its `cc -O2`
backend run — the whole pipeline to a runnable binary, which is the honest thing
to measure even though it's the unflattering one.

Rust is invoked as bare `rustc` on a single file, **not** `cargo build`. Cargo
would add manifest parsing, lockfile resolution and dependency work that has
nothing to do with compiler speed, and charging that to Rust would have made
machin look good for a bad reason.

### † Zig's column is not a result

Do not read "machin builds 130× faster than Zig" out of that column. On this
machine:

| what | time |
|---|--:|
| `std.debug.print` + `-OReleaseFast` | **12.6–13.8 s** |
| `std.debug.print` + Debug | 0.53 s |
| fib **without** printing + `-OReleaseFast` | 0.22 s |
| empty program + `-OReleaseFast` | 0.22 s |

The entire cost is compiling std's formatting machinery under `ReleaseFast`, and
on this install it is **not reused between two identical consecutive runs** —
including with an explicit writable `--global-cache-dir`, which fills to 45 MB and
still doesn't help. This is zig 0.16.0 (beta, snap).

That is a fixed std-formatting tax on a beta toolchain, not a statement about how
Zig scales with your code, so this benchmark reports it and draws no conclusion
from it. Zig's build times are normally one of its selling points.

## Binary size — machin wins big, dynamically

As produced / stripped:

| kernel | machin | Rust | Zig |
|---|--:|--:|--:|
| fib | 17 kB / **14 kB** `[dyn]` | 4291 kB / 335 kB `[dyn]` | 3508 kB / 491 kB `[static]` |
| mandel | 17 kB / **14 kB** `[dyn]` | 4291 kB / 336 kB `[dyn]` | 3511 kB / 491 kB `[static]` |
| sieve | 17 kB / **14 kB** `[dyn]` | 4292 kB / 336 kB `[dyn]` | 3527 kB / 498 kB `[static]` |
| intsum | 17 kB / **14 kB** `[dyn]` | 4291 kB / 335 kB `[dyn]` | 3510 kB / 491 kB `[static]` |

**Compare like with like.** machin and Rust both link dynamically against the
system libc, so their stripped numbers are directly comparable: **14 kB vs
335 kB — about 24× smaller.** There is no std runtime to link; machin's output is
C, and C's runtime is already on the machine.

The as-produced column (17 kB vs 4291 kB, ~250×) is a much bigger number and a
much worse comparison — it mostly measures how much debug info each toolchain
leaves in by default. It's shown for completeness, not as the headline.

Zig links statically here, so it carries libc the other two borrow. That is not a
loss for Zig: **fully static, Zig wins.** `machin build --static` produces
940 kB against Zig's 491 kB, because machin's static build bundles SQLite.

## The honest read

Two clear results in opposite directions:

- **Binary size is a genuine machin win** — 24× smaller than Rust at equal
  linkage, and it holds across every kernel. If you ship containers or care about
  cold start, this is the number that matters (see also
  [`../cold-start`](../cold-start)).
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
