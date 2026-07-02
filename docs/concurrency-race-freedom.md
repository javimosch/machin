# Machin: inferred data-race freedom — the "better than Rust" concurrency arc

Machin already ships Go-style concurrency (`go` / channels / `select`, pthread runtime).
It ships **zero** race analysis — a textbook data race passes `machin check` silently.
Rust closes that hole with `Send`/`Sync` + the borrow checker, a tax the *human* pays.
The thesis of this arc: **machin infers data-race freedom with no annotations**, because
the agent and the compiler are one loop. Same guarantee as Rust; none of the ceremony.

**Phase 0 (done, branch `concurrency-race-spike`)** validated the approach: a ~200-line
mutation-summary + goroutine-escape pass flags every racy shape (two goroutines share a
mutated slice; goroutine + this-thread write; transitive mutation) with a counterexample,
and passes every safe shape (distinct slices, single writer, share-by-communicating). It
also confirmed the two load-bearing semantic facts:

- **Slices are shared references** — a callee's `xs[i] = v` mutates the caller's slice.
- **Structs pass by value, but a slice *field* stays shared** — `x.n` write on a struct
  param does not escape; `x.items[i]` write does. So sharing is **reachability-based**.

---

## Phase 1 — the real inferred race-freedom checker (reference compiler)

Goal: a **sound**, type-aware data-race analysis in the Go reference compiler, surfaced as
first-class diagnostics through `machin check` / `machin check --json`, precise enough to
run clean on the real concurrency corpus. Soundness stance is a borrow-checker's:
over-approximate (never miss a real race), then add precision to cut false positives.

### The correct data-race definition (fixes a spike blind spot)
A data race = **two concurrent accesses to the same shared heap location, at least one a
write, with no synchronization between them.** The spike only tracked *writers* — it misses
a goroutine that *reads* a slice while the main thread writes it. Phase 1 tracks **reads and
writes**, and flags a root with ≥2 concurrent accessors where ≥1 is a writer.

### Sharing classification (the reachability rule)
A place races only if its value can be aliased across a goroutine boundary. Define
`sharesHeap(T)`: true iff `T` is a slice, map, or channel, **or** a struct/array that
transitively contains one. Scalars, bools, strings (immutable), and pure-scalar structs are
copied on the goroutine boundary → can never race. This needs the checker's resolved types,
so the pass runs **after** typecheck (see Integration).

### Slices

**1.1 — Type-aware accesses + read/write races.**
- `sharesHeap(T)` over resolved types (reuse the checker's type table + struct defs).
- Per goroutine-reachable root, record **reads and writes** (not just writes); a field
  write counts only when it reaches shared heap (`s.items[i]`, not `s.n`).
- Race = root with ≥2 concurrent accessors, ≥1 writer. Emit a counterexample naming the
  variable, each accessor (goroutine vs this-thread, read vs write), and the sites.
- Tests: write/write, **read/write**, pure-scalar-struct-field write is CLEAN,
  slice-field write RACES.

**1.2 — Globals + move-on-send.**
- Package-level `var` globals of shared type are shared across *all* goroutines by
  definition: a goroutine write + any concurrent access = race (a big real-world case the
  spike didn't cover).
- **Move-on-send**: `ch <- v` transfers ownership of `v`. Two consequences: (a) using `v`
  on the sender after the send is a *use-after-move* diagnostic; (b) a value moved into
  exactly one goroutine and never touched again by the spawner is provably safe. This is
  the formal backbone of "share by communicating."

**1.3 — Closure captures** (reference compiler already supports closures).
- `go func(){ ... }()` capturing a slice/map by reference shares it. Extend the escape
  analysis from call-args to **captured variables** and `CallValue`. (The self-hosted
  compiler lacks closures — this is reference-only until Phase 3's closure port.)

**1.4 — Happens-before precision (false-positive reducer).**
- Recognize the **channel-join idiom**: a `<-done` receive (or `range done`) that waits on a
  goroutine establishes happens-before, so accesses *after* the join don't race with it.
  Sound to omit (just less precise) → ship 1.1–1.3 first, add this to quiet real code.
- Same for a spawn inside a loop where each iteration owns a distinct element.

**1.5 — Surface through `machin check` + `--json`.**
- New diagnostic family `RACE` (e.g. `RACE001` write/write, `RACE002` read/write,
  `RACE003` global race, `RACE004` use-after-move), `phase: "race"`, counterexample in
  `message`. Fold into `analyzeSource` / `CheckResult` so `--json` carries them.
- **Corpus validation** — the real test: run against the five concurrency apps
  (machin-healthcheck, machin-linkcheck, machin-pipe, machin-pool, machin-wscat). Target:
  **zero false positives** on correct programs, or a genuine race surfaced. Plus expanded
  unit tests (the spike's six, grown to cover 1.1–1.4).

### Integration points (Go reference compiler)
- **Types**: the pass needs resolved types for `sharesHeap`. Hook the `Checker` after it
  finishes (`types.go`) and expose a `typeOfPlace(fn, expr)` query, or build a light place-
  typer from signatures + struct defs + `:=` initializers. Prefer reusing the checker's
  table to stay in sync with real inference.
- **Pipeline**: invoke the race pass from the same place `machin check` runs typecheck
  (`check.go` `analyzeSource`), gated so a parse/type error short-circuits before it.
- **Reuse**: the spike's `rsMutSummary` (fixed-point mutation summary) and `rsExprRoot`
  graduate into the pass, now type-filtered and extended to reads + globals + captures.

### The one decision for you (severity / gating)
To *be* a Rust-grade guarantee, a race must **block compilation** like Rust does. But the
existing corpus may contain benign/intentional races, and flipping to hard-error mid-arc
could break `machin build`. Options:
- **(a) Warn in `check`, gate hard-error behind `--race-safe`** (and a per-file pragma).
  Non-breaking; "guarantee mode" is opt-in first, default later. *Recommended.*
- **(b) Hard error in both `check` and `build` immediately.** Strongest claim, highest risk
  of breaking corpus programs until they're audited.
- **(c) Warn only.** Weakest claim; easy but undersells the differentiator.

Recommend (a): ship the analysis as errors *in `check`* (the credibility surface) with build
gated behind `--race-safe`, then promote to default once the corpus is clean.

### Verification gate for Phase 1
1. Unit suite (expanded spike) green: every racy shape flagged, every safe shape clean,
   incl. read/write, global, move-after-send, scalar-struct-field-clean.
2. Zero false positives across the five concurrency corpus apps (with 1.4 precision on).
3. `machin check --json` emits well-formed `RACE` diagnostics with counterexamples.
4. `go test .` green; existing check/codegen parity unchanged.
5. No change to `machin build` default behavior under option (a).

### Out of scope for Phase 1 (later phases)
- **Phase 2**: credibility artifact — blog + intrane.fr ("Go's concurrency, Rust's
  guarantee, neither's tax"), live counterexample demo.
- **Phase 3**: self-hosted parity — port `go`/`chan`/`select` codegen (#280) **and** the
  race pass into the self-hosted checker, so self-hosted machin both compiles concurrency
  and proves it safe.
