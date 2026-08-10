# Benchmark — native speed vs Rust & Zig

machin compiles MFL **through C** (`cc -O2`) to a native binary, so its runtime
*is* whatever the C optimizer produces. The README claims "C/Rust-class speed."
This puts a number on it against the two reference native toolchains — **Rust**
and **Zig** — on four compute kernels.

Every kernel produces **byte-for-byte identical output** in all three languages
(verified each run), so this times the *same computation*, not three different
ones.

## Results (this machine — gcc 11.4, rustc 1.98, zig 0.16; min of 9 interleaved rounds)

| kernel | what it stresses | machin | Rust `-O3` | Zig `ReleaseFast` | verdict |
|---|---|--:|--:|--:|---|
| **fib(40)** | recursion / call overhead | **214.7 ms** ✦ | 289.7 ms | 292.9 ms | **machin faster by 26%** |
| **mandel** 1000² | float tight loop | 724.2 ms | 720.1 ms | **702.8 ms** ✦ | tie (machin 1.03×) |
| **sieve** 10⁷ | array build + indexing | 202.0 ms | 142.9 ms | **137.9 ms** ✦ | machin slower, 1.46× |
| **intsum** 10⁹ | integer ALU (mul+mod) | **3079.7 ms** ✦ | 3223.8 ms | 3189.7 ms | tie (machin +3%) |

✦ = fastest. **machin wins 1 clearly, ties 2, loses 1.**

Absolute milliseconds are machine-specific; the **ratios** are the portable
result. Worst observed run-to-run spread on this laptop was 41% of the min
sample, which is why the harness reports anything under 3% as a tie rather than
declaring a winner inside the noise.

### A methodology fix worth knowing about

An earlier version of this benchmark ran all N samples of machin, then all N of
Rust, then all N of Zig. On a laptop that heats up and down-clocks during a
multi-second kernel, **that penalizes whichever language runs last** — an
ordering artifact, not a speed difference. The harness now interleaves: one
round of (machin, rust, zig), then the next, rotating who starts each round.

Re-measuring that way changed the story. The previous README claimed machin won
the integer-sum loop "by ~20–25%"; interleaved, that margin is **3% — a tie**.
The claim has been corrected here and in `machin guide`.

## The honest read

machin is **native, full stop** — it competes in the same tier as Rust and Zig,
not the scripting tier. Because it *is* C underneath:

- On **scalar recursion** (`fib`) `gcc -O2` on machin's generated C beats
  `rustc -O3` and Zig `ReleaseFast` by a clear margin on this CPU.
- On the **float kernel** and the **integer loop** it is a dead heat — inside
  the noise band in both directions.
- On the **sieve** machin trails by 1.46×. See below: this is real, but it is
  not what it looks like.

The point is not "machin beats Rust" — it doesn't, in general. It's that machin
is squarely in the **compiled-native performance class**, with no VM, no
interpreter, unboxed values — and it gets there from source an AI agent writes
about as cheaply as Python (see [`../rest-sqlite`](../rest-sqlite)).

## The sieve gap was `append`, and it is now largely closed

This README used to explain the sieve loss as *"its slice indexing/layout is less
optimal than a Rust `Vec` or a Zig slice."* **That was wrong.** Phase-timing showed
slice indexing tying Rust exactly, with the whole gap in building the array:

| phase | machin (before) | Rust |
|---|--:|--:|
| build the 10M array by `append` | **70–83 ms** | **27–29 ms** |
| the sieve loop itself | 110 ms | 111 ms |
| the count loop | 5–7 ms | 3 ms |

`mfl_realloc` could only ever *copy* into a fresh arena block, because an arena
frees nothing mid-life — so growing to 10M elements walked ~21 doublings, each
allocating and memcpy'ing and never releasing the previous buffer. `Vec::push`
hands the block to `realloc`, which extends it in place via `mremap`.

**Fixed in #578.** `machin alias` proves, fail-closed, that a local slice has no
other live reference across an append; where it does, codegen grows the block in
place instead. Measured on the CI runner (a quieter machine than this laptop),
before and after, normalized against Rust because the runner drifted ~15% slower
between sessions:

| | machin | Rust | Zig | machin/Rust |
|---|--:|--:|--:|--:|
| before | 96.7 ms | 87.7 ms | 73.0 ms | **1.10×** |
| after | 101.9 ms | 100.6 ms | 86.0 ms | **1.01×** |

**The sieve now ties Rust.** It does *not* tie Zig, which is still ~1.19× faster
on this kernel — down from 1.32×, but a real remaining gap and reported as one.

The tempting version of this fix — hand any arena block straight to `realloc` — is
**unsound**, because slices share backing storage (`b := a` aliases, and so does
passing a slice to a function). That is why it is gated on a proof rather than
done unconditionally, and why an adversarial suite runs under AddressSanitizer in
CI.

## Fairness notes

- Each language at its **standard release** setting; Rust at `opt-level=3` (its real
  release level, not the weaker `-O`/level-2), Zig at `ReleaseFast`. machin has one
  setting: `cc -O2`.
- **No `-march=native`** for anyone — machin's build doesn't use it, so neither do
  the references.
- Same algorithm, same constants, same loop structure; integer kernels use 64-bit
  elements in all three so the sieve compares codegen, not element size.
- Zig's modulo uses `@rem` (truncated, like C/Rust `%`), not `@mod` (Euclidean),
  which would emit extra sign-handling.
- Timing is **interleaved and rotated**, and differences under 3% are reported as
  ties.

## Reproduce

```bash
./run.sh        # builds all 12, checks identical output, prints the timing table
```

Needs `machin`, `cc`, `rustc`, `zig`, and `python3`.

See also [`../compile-speed`](../compile-speed) (how long the *build* takes) and
[`../evidence`](../evidence) (what each compiler tells you about a bug).
