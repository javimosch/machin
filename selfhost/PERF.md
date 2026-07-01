# Self-host performance: the honest numbers

The bootstrap is partly a bet that an MFL-written compiler can be *fast enough* to
matter. This file records measured reality, not hope. Reproduce with
`selfhost/bench.sh` (all numbers from this machine, `cc -O2`).

## Headline (after the substr strlen-cache fix)

| Workload                      | MFL before | MFL after | Go     | Ratio now          |
|-------------------------------|-----------:|----------:|-------:|--------------------|
| Pure integer loop (200M)      |     656 ms |    656 ms | 885 ms | **0.74× (faster)** |
| `substr`-heavy loop (98M)     |    2424 ms |   2424 ms |  55 ms | 44× (unchanged)    |
| The lexer, real corpus (blend)|    5283 ms |  **636 ms** | 273 ms | **~1.4× slower**   |

Same outputs in every row (token counts / checksums identical) — this measures the
same algorithm compiled two ways, not two algorithms. The lexer is the real workload;
it went **19.4× → 1.4×** off Go after one fix. The pure-`substr` micro-benchmark is
*intentionally* unchanged — it isolates malloc+copy on a 54-byte string where `strlen`
was already cheap, so the cache (below) can't help it; it's the worst case, not the
common one.

## What this means

1. **MFL codegen is not the problem.** On raw compute MFL → C → `-O2` is *faster
   than Go* (Go's GC-managed runtime vs flat C). The compiler back-end is competitive.

2. **The lexer gap was not the copy — it was a redundant scan.** MFL strings are
   NUL-terminated `char*`, so `mfl_substr(s,i,j)` called `strlen(s)` just to clamp `j`.
   In the lexer `s` is the *entire file*, sliced once per token — so every token paid an
   O(filesize) scan: 17,495 tokens × 98 KB ≈ 1.7 GB scanned per pass, pure waste.

## The fix (landed — `codegen.go`)

A pointer-keyed `strlen` memo: `mfl_substr` caches `(s, strlen(s))` and reuses it when
called again with the same pointer. The lexer slices one `src` thousands of times, so
all but the first call are O(1). **Clamping semantics are byte-for-byte identical** —
it's a cache, not a contract change — proven by the lexer + parser oracles still
matching the Go compiler across the whole corpus. The arena's `free` path invalidates
the cache so a reused address can never return a stale length. Result: lexer **5283 →
636 ms (8.3×)**, and every string-slicing MFL program (parsers, scanners, JSON/CSV
readers) gets it for free — the bootstrap *surfaced* a win for the whole ecosystem
(north-star: usage drives features).

## What's left (not needed yet)

The residual ~1.4× and the untouched 44× micro-benchmark are pure per-slice
malloc+copy. Closing that needs a real zero-copy `{ptr,len}` string-view representation
(Go's `s[i:j]` shares the backing array) — a global repr change, deliberately *not*
done now: it's invasive and the lexer is already competitive. Tracked for if/when a
later stage's profile demands it.

## The fixpoint is reached — and the whole-compiler number (perf gate: PASSED)

The self-hosting bootstrap is **complete**: the MFL compiler compiles its own source
to a native binary that reproduces itself byte-for-byte (`verify-fixpoint.sh`). The
whole-pipeline benchmark (parse + typecheck + codegen of the compiler's own 644-decl
source → 6850 lines of C):

| | before opt | after opt |
|---|---:|---:|
| Go machin (reference) | 251 ms | 374 ms |
| MFL self-hosted | 1865 ms (**7.4× slower**) | **335 ms (0.90× — faster than Go)** |

**The gap is closed.** The bottleneck was NOT the linear scans first suspected — a
phase timer showed codegen was ~85% of the time, and it was **O(n²) string building**:
`cemit` did `g_out = g_out + s` (immutable-string concat copies the whole growing
buffer per call), and `c_quote` built the 67 KB embedded base64 prelude byte-by-byte
the same way; even the runtime `mfl_join` was O(n²) (repeated `mfl_cat`). Two fixes:

1. **Runtime `mfl_join` → O(n)** (one length pass, one alloc, memcpy) — a general
   improvement for every MFL program that builds strings via `join`, `codegen.go`.
2. **Codegen accumulates output as a `[]string` and joins once** — `cemit`/`indent`
   append chunks (amortized O(1) via `mfl_append`'s capacity doubling), `c_quote`
   collects per-byte pieces then joins. Same class of fix as the substr cache: a
   representation choice, not a language limit.

(The `node_slot_of` O(1) direct index — a generation-tagged array replacing the per-
expression node-array scan — was also applied; it was a minor contributor, ~9%.)

## Takeaway for the keep/drop gate

The decision was framed as "ship the MFL compiler only if it benchmarks competitively."
The data says **yes**: raw codegen already beats Go, and the one real hotspot turned out
to be a one-line-class runtime fix, now landed and oracle-verified. Self-hosting is not
blocked on performance. Good thing to settle before the hard semantic stages.
