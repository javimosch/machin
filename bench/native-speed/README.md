# Benchmark — native speed vs Rust & Zig

machin compiles MFL **through C** (`cc -O2`) to a native binary, so its runtime
*is* whatever the C optimizer produces. The README claims "C/Rust-class speed."
This puts a number on it against the two reference native toolchains — **Rust**
and **Zig** — on four compute kernels.

Every kernel produces **byte-for-byte identical output** in all three languages
(verified each run), so this times the *same computation*, not three different
ones.

## Results (this machine — gcc 11.4, rustc 1.98, zig 0.16; min of 5 runs)

| kernel | what it stresses | machin | Rust `-O3` | Zig `ReleaseFast` | machin vs best |
|---|---|--:|--:|--:|--:|
| **fib(40)** | recursion / call overhead | **245 ms** ✦ | 303 ms | 306 ms | **1.00×** |
| **mandel** 1000² | float tight loop | 827 ms | **814 ms** ✦ | 819 ms | 1.02× |
| **sieve** 10⁷ | array indexing / memory | 203 ms | 153 ms | **145 ms** ✦ | 1.40× |
| **intsum** 10⁹ | integer ALU (mul+mod) | **2832 ms** ✦ | 3764 ms | 3556 ms | **1.00×** |

✦ = fastest. **machin wins 2, ties 1, loses 1.**

## The honest read

machin is **native, full stop** — it competes in the same tier as Rust and Zig,
not the scripting tier. Because it *is* C underneath:

- On **scalar recursion and integer loops** (`fib`, `intsum`), `gcc -O2` on
  machin's generated C matches or **beats** `rustc -O3` and Zig `ReleaseFast` here
  — by ~20–25 %. This is gcc's optimizer on straightforward C, and on this CPU it
  wins these two.
- On the **float kernel** it's a dead heat (within 2 %).
- On the **array-heavy sieve** machin trails by ~1.4× — its slice indexing/layout
  is less optimal than a Rust `Vec` or a Zig slice. A real, current limitation, not
  hidden.

The point is not "machin beats Rust" (it doesn't, in general — the sieve shows
that). It's that machin is squarely in the **compiled-native performance class**,
with no VM, no interpreter, unboxed values — and it gets there from source an AI
agent writes about as cheaply as Python (see [`../rest-sqlite`](../rest-sqlite)).

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

## Reproduce

```bash
./run.sh        # builds all 12, checks identical output, prints the timing table
```

Needs `machin`, `cc`, `rustc`, `zig`, and `python3`.
