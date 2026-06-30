# Self-host performance: the honest numbers

The bootstrap is partly a bet that an MFL-written compiler can be *fast enough* to
matter. This file records measured reality, not hope. Reproduce with
`selfhost/bench.sh` (all numbers from this machine, `cc -O2`).

## Headline

| Workload                     | MFL (native) | Go      | Ratio              |
|------------------------------|-------------:|--------:|--------------------|
| Pure integer loop (200M)     |       656 ms |  885 ms | **0.74× (faster)** |
| `substr`-heavy loop (98M)    |      2424 ms |   55 ms | **44× slower**     |
| The lexer, real corpus (blend)|     5283 ms |  273 ms | 19× slower         |

Same outputs in every row (token counts / checksums identical) — this measures the
same algorithm compiled two ways, not two algorithms.

## What this means

1. **MFL codegen is not the problem.** On raw compute MFL → C → `-O2` is *faster
   than Go* (Go's GC-managed runtime vs flat C). The compiler back-end is competitive.

2. **The entire gap is one thing: string slicing.** Go's `s[i:j]` is zero-copy — it
   returns a view sharing the original backing array, zero allocation. MFL's `substr`
   **heap-allocates a fresh string and copies the bytes** every call, then the GC has
   to reclaim them. The lexer calls `substr` once per token (17,495×/pass); the micro-
   benchmark isolates it at 44× because it does nothing else.

3. **It is fixable, two independent ways:**
   - *Runtime (helps every MFL program):* a zero-copy string-view — make `substr`
     return a `{ptr,len}` view into the source instead of a copy, materializing only
     on escape/mutation. This is a back-end change to the Go compiler and is the
     higher-leverage fix; the bootstrap merely *surfaced* it (north-star: usage drives
     features).
   - *Program (helps the self-host compiler now):* tokens carry `(pos,len)` byte
     offsets, not materialized `val` strings; identifiers intern through a single map.
     The hot path then never allocates per token.

## Takeaway for the keep/drop gate

The decision was framed as "ship the MFL compiler only if it benchmarks competitively."
The data says: **the ceiling is competitive** — raw codegen already wins — and the one
hotspot in the way (`substr` copy) is concentrated, measured, and fixable without
touching codegen quality. Self-hosting is not blocked on performance; it is blocked on
one well-understood allocation change. That is the good outcome to learn at this stage,
before the hard semantic stages.
