# Benchmark — what the compiler tells you

`native-speed` measures how fast the program runs. `compile-speed` measures how
long you wait for a binary. This one measures the thing you cannot put a
millisecond on: **which toolchain hands you the bug, and when.**

Two programs, each written the natural way in machin, Rust and Zig, with the same
semantics in all three. For each we record the verdict at **build** time and at
**run** time.

## Case 1 — deadlock: block on a value that can never arrive

```
machin   ch := make(chan int); v := <-ch      // nothing ever sends
Rust     rx.recv() with the sender still alive
Zig      sem_wait() on a semaphore nobody posts
```

| | build-time verdict | run-time behaviour |
|---|---|---|
| **machin** | **`DL001` — "receive on channel `ch` that is never sent to or closed, a guaranteed deadlock"** | **exits 2** with a causal report naming the blocked goroutine and the channel |
| Rust | accepted, no diagnostic | **hangs forever** (killed at 5 s, exit 124) |
| Zig | accepted, no diagnostic | **hangs forever** (killed at 5 s, exit 124) |

machin's runtime report:

```
fatal: deadlock — all 1 goroutine(s) blocked, none can make progress:
  goroutine 0     waiting to receive on channel #0
```

This is the clearest win in the suite, and it is worth being precise about why.
Rust's type system prevents **data races** — it does not prevent **deadlocks**,
and the Rust project has never claimed it does. A `recv()` that can never return
is a well-typed, `unsafe`-free, perfectly idiomatic Rust program. Zig doesn't
attempt either. In both, the failure mode is a process that hangs until something
outside it notices.

machin catches this one twice: `DL001` at compile time (sound and
false-positive-free — it only fires when it can *prove* nothing ever feeds the
channel), and a runtime quiescence detector that turns any remaining deadlock
into an exit code and a wait-cycle instead of a hang.

**The honest limit:** `DL001` proves nothing in the clean case. A channel is
treated as fed unless the analysis can prove it never is, so a clean run is not a
proof of deadlock-freedom — it is the runtime detector that catches the rest.

## Case 2 — an out-of-range index, before it is ever hit

```
func at(xs, i) (r) { r = xs[i] }     // index comes from the environment
```

### At build time

| | verdict |
|---|---|
| **machin** | **`FALS001` index out of range at `xs[i]`, with a concrete failing input** |
| Rust | silent — bounds are a runtime concern |
| Zig | silent |

```
[FALS001] index out of range at `xs[i]` when xs=[]int{}, i=0 — in at (line 1)
```

Neither `rustc` nor `zig` says anything about `at()`. `machin falsify` enumerates
small concrete inputs and reports one that breaks it — **before the program is
ever run**, and on code whose only call site (`at(xs, 1)`) is perfectly in range.

**The honest limit:** falsify is *unsound-complete* — it finds bugs, it never
proves their absence. A clean result means "no counterexample within the bounds",
never "correct". And the witness it reports here is `xs=[]int{}, i=0`, not the
`IDX=5` a user would eventually type: it proves the function is unsafe for *some*
input, and hands you the smallest one, not the one production will hit.

### At run time

| | `IDX=1` | `IDX=5` (out of range) | exit |
|---|---|---|---|
| machin (default) | 20 | **`0` — silently wrong** | 0 |
| machin `--safe` | 20 | `panic: index out of range [5] with length 3` | 1 |
| **Rust (release)** | 20 | `panicked at oob/at.rs:5:5` | 101 |
| Zig `ReleaseFast` | 20 | **`281479271677952` — silently wrong** | 0 |
| Zig `ReleaseSafe` | 20 | `panic: index out of bounds: index 5, len 3` | 134 |

**Read this row honestly: Rust wins it.** Rust keeps its bounds check in release
and traps by default. machin's default build omits the check exactly like Zig's
`ReleaseFast` — it printed `0` and exited **successfully**, which is the same
class of silent-wrong as Zig reading `281479271677952` out of adjacent memory.
Both need an opt-in (`--safe`, `ReleaseSafe`) to trap.

An earlier version of this benchmark compared machin `--safe` against Zig
`ReleaseFast` and made machin look safe by default. It isn't. That comparison was
removed.

Note also that the flag which makes Zig fastest — `ReleaseFast`, the one
[`../native-speed`](../native-speed) uses for its timings — is the one that turns
this into undefined behaviour.

## The honest read

Stack the two cases up and the shape is clear, in both directions:

- **machin is alone in reporting either bug at build time.** Neither `rustc` nor
  `zig` has a counterpart to `DL001` or `FALS001`. That is the real differentiator
  — not speed, where the three are in the same tier.
- **Rust is the safest at runtime by default**, and machin is not. machin's
  default build will read out of bounds as happily as Zig's.
- **Neither Rust nor Zig catches the deadlock at all**, at build time or run time.
  They hang.

So the claim machin can honestly make is *"it tells you earlier"*, not *"it is
safer"*. Earlier matters — a bug reported at build time with a concrete input
costs minutes, and the same bug reaching production as a hung process or a silent
`0` costs much more. But it is a different claim from Rust's, and it does not
replace Rust's.

## Reproduce

```bash
./run.sh
```

Needs `machin`, `rustc`, `zig`. Hanging programs are killed with `timeout`; a kill
(exit 124) is a result, not a harness failure.

> Both Zig programs use POSIX APIs through `std.c` (`sem_wait`, `getenv`) rather
> than Zig's own equivalents. Zig 0.16 is mid-redesign: concurrency primitives
> moved under `std.Io` and now require an `Io` instance, `std.os.argv` is gone, and
> `std.process`'s argument iterators want an allocator. The POSIX calls are stable
> and express exactly the same program.
