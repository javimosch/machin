# The Falsifier — Phase 1 plan

*Compile-time bounded bug-finding that hands the agent a concrete failing input
and a runnable repro. Phase 0 spike validated the mechanism; this is the plan to
turn it into a real, shipped `machin` capability.*

## Thesis

An agent's dominant failure mode is **plausible-but-wrong** code. Agents write
specs well and judge correctness poorly. The Falsifier makes the compiler an
**adversary**: on `machin check`/`falsify` it bounded-explores each function and,
when a property can break, returns the exact input that breaks it plus a runnable
`.mfl` repro that auto-promotes to a regression test. Neither Rust (Kani/Miri are
bolt-ons ~nobody runs in CI) nor Zig (runtime checks in debug mode) ships this as
a default. It composes with the race-freedom arc into one story: **machin
binaries come with evidence.**

## What Phase 0 proved (baseline we build on)

- A pure checker over the **existing typed IR** (`ParseProgram` + `Check`) finds
  div-by-zero, mod-by-zero and index-out-of-bounds with correct concrete
  counterexamples, in **microseconds**, zero false positives on correct code.
- Every counterexample, emitted as a `--safe` repro, **panics at exactly the
  predicted trap** in the real runtime — findings are real bugs, not artifacts.
- Reference: `falsify_spike.go` / `falsify_spike_test.go` on branch
  `falsify-spike` (throwaway Go, like `racecheck.go` was pre-port).

## The one design decision that shapes everything

**The Falsifier is UNSOUND-complete: it finds bugs, it does not prove their
absence.** That is the opposite of the race pass (which was sound and could
therefore hard-fail `build` behind `--race-safe`).

Consequence — and this is a hard rule for Phase 1:

- Falsify findings are **diagnostics (warnings)**, surfaced in `check --json` and
  a dedicated `machin falsify` command. They **must NOT hard-fail `build` by
  default.** A bounded checker that gates builds would reject correct programs
  whose bug it merely couldn't rule out. Optional strict mode (`--falsify-strict`
  fails on any counterexample) can exist, off by default.
- Verdicts are honest and three-valued from day one, even though Phase 1 only
  emits two of them: `counterexample` (found) and `unknown` (bounded-out /
  unsupported). `proved(k≤N)` is explicitly deferred to a later phase (needs a
  symbolic/path upgrade); Phase 1 never claims it.

## Diagnostic contract (stable, agent-facing)

New `Phase: "falsify"`. Codes (branch on these, never the message):

| Code | Property |
|---|---|
| `FALS001` | index out of range (slice/string read or assign) |
| `FALS002` | divide / modulo by zero |
| `FALS003` | nil deref (nil map/slice/struct-ptr use) |
| `FALS004` | integer overflow (opt-in; noisy — gate behind a flag) |

Each finding carries, in addition to the existing `Diagnostic` fields
(`Severity`/`Phase`/`Code`/`Message`/`Decl`/`Line`/`Snippet`):

- `Message`: rendered as `<prop> at \`<expr>\` when <p1>=<v1>, <p2>=<v2>` (matches
  the spike's counterexample string).
- `Line`: attached per-decl via the existing `declLine[finding.Decl]` mechanism
  (same as `detectRaces` in `check.go`), so no AST-position surgery is required
  for v1. Column/expr-precise positions are a stretch goal (see Slice 1.1).

`Severity` stays `"error"` in the struct for wire-compat but these are advisory;
document that `phase=="falsify"` diagnostics are non-fatal unless `--falsify-strict`.

## Slices

Modelled on the race arc (Go reference → integrate into `check` → self-host port
→ blog). Each slice is independently shippable and corpus-validated.

### Slice 1.1 — `falsify.go`: promote the spike to a real Go pass — ✅ DONE (commit 452c76d)
- Move the spike interpreter/enumerator into a non-throwaway `falsify.go` with an
  exported `detectFalsifiable(prog, c) []falsifyFinding` mirroring
  `detectRaces`'s signature and finding struct.
- Fix the two known spike defects:
  - **Pretty-printer precedence** (`100 % (a-b)` printed `100 % a - b`): add
    parens by precedence when rendering `expr`.
  - **String-range value** currently treated as int; make range-over-string bind
    a 1-char string (matches MFL semantics) — or drop string bodies from scope
    and mark `unknown`.
- ~~Add `FALS003` nil-deref~~ **deferred to Slice 1.3.** MFL has value semantics
  and no source-level null pointers; the only nil surface in 1.1 scope is a nil
  slice, whose index already traps as `FALS001`. A distinct `FALS003` earns its
  keep only once maps (nil-map write) and structs land in 1.3, so it moves there.
- **Domain generator**: extract the `domain(slot)` logic; make bounds a single
  named policy (`falsIntDomain`, `falsSliceLenMax=3`, `falsSliceElemVals`, …) so
  the honesty story is auditable. ✅
- **Cost cap + `unknown`**: mixed-radix product cap + per-input step budget; on
  trip, the input is `unknown` (never silently counted clean). `falsifyStats`
  exposes `Skipped`/`AllUnknown`; the user-facing `log()` of drops lands with the
  verdict envelope in Slice 1.2. ✅
- **Key hardening beyond the plan**: the interpreter marks any unmodeled node or
  call `unknown` and never reports it, so a counterexample is emitted only from a
  fully-modeled concrete path — no stubbed value can manufacture a false positive.
- Test: permanent `falsify_test.go` — `TestFalsifyFinds` (planted bugs +
  `--safe` repro-panics), `TestFalsifyOperators` (operator families + unknown
  paths), `TestFalsifyHelpers`, `TestFalsifyDiagnostic`. Package coverage 87.60%
  (floor 87.00%); `go test .` green. ✅

*Scope note carried into 1.2/1.3:* the two spike defects are fixed
(precedence-correct render, range-over-string binds a 1-char string).

### Slice 1.2 — integrate into `check` + the `machin falsify` driver — ✅ DONE

Implemented: `CheckResult.Warnings` (advisory channel — falsify findings never
touch `ok`/`errorCount`/exit code); `analyzeSource` runs `detectFalsifiable`
after the race pass on a clean typecheck; `emitCheck` prints warnings in human
mode; `machin falsify` driver (`falsifycmd.go`) with `--json` verdict envelope
`{ok, counterexamples, findings, coverage:{checked,skipped,allUnknown}}` (never
claims `proved`) and `--repro <dir>` (drops any existing `main`, writes runnable
repros that panic under `--safe`). Docs (`check-json.md` — new phase/codes +
`warnings` field + falsify section) and `guide.go` (falsify verb + `check`
description + `falsification` gotcha) updated. Version bump held for the 1.5
release. Tests: `TestFalsifyInCheck`, `TestFalsifyCmdJSON/Repro/Errors`. Full
`go test .` green; coverage floor held.

<details><summary>original plan</summary>
- In `analyzeSource` (`check.go`), after the race pass, run `detectFalsifiable`
  and append findings as `phase:"falsify"` diagnostics, `Line` via `declLine`.
  Default on for `check --json` (it's advisory); gated off `build`.
- New `case "falsify"` in `main.go` dispatch: `machin falsify <file>` prints the
  human-readable counterexamples; `machin falsify --json <file>` emits the
  diagnostic array; `machin falsify --repro <dir> <file>` writes one runnable
  `.mfl` repro per finding (the spike's `reproProgram`, `println(str(...))`
  form). Repros are the auto-promotable regression tests.
- Update `docs/check-json.md` (new phase + codes) and `guide.go` (version bump +
  a `falsify` gotcha/verb entry so agents discover it in-binary).
</details>

### Slice 1.3 — widen the domain: structs + interprocedural — ✅ DONE (maps deferred)

Implemented **interprocedural inlining** and **struct params**:
- **Inlining**: a user call is no longer `unknown` — the callee is interpreted in
  a fresh frame (args by value), so a bug that manifests only through a call is
  found and reported against the *enclosing* function (a callee that hits anything
  unmodeled still makes the whole path inconclusive). Recursion is capped
  (`falsCallDepth`) → `unknown`, never a hang. `abs` is modeled; other builtins
  stay `unknown`. `main` is now a valid target (its repro is the program verbatim).
- **Structs**: `fval` gains a struct variant; `StructLit` construction (named +
  positional + partial), `FieldAccess`, and `FieldAssign` are interpreted. Struct
  values get **value semantics** via `clone()` on every bind/param (slice fields
  keep shared backing, per MFL) — so mutating a copy can't alias the original, a
  false-positive hazard. `structDomain` enumerates bounded field tuples (small
  per-field domains, product capped at `falsStructDomCap`); counterexamples render
  as real struct literals (`Cfg{n: 0}`) so `--repro` stays valid.
- **Proven on the corpus**: `machin falsify examples/complex/multi_return.mfl`
  finds two genuine latent bugs — `divmod(a,b)` divides by zero at `b=0`, and
  `minmax(xs)` indexes `xs[0]` on an empty slice — neither guarded by the source.
- Tests: `TestFalsifyStructsAndCalls` (interproc bug, struct-field div + index,
  value-semantic copy = no FP, unbounded recursion = inconclusive), each `found`
  case verified by a repro that panics under `--safe`.

**Maps deferred** (documented follow-up, like FALS003). Two reasons they don't
fit the 1.3 guarantees: (1) maps are an MFL *reference* type with
nondeterministic range order — modeling them without risking false results needs
canonicalized iteration and careful aliasing; (2) there is **no map literal**, so
a map counterexample cannot become a clean `target(args)` repro (it needs
multi-statement `make`+assign construction) — which would break the
repro-panics-under-`--safe` firewall that keeps the pass honest. Structs (7 corpus
files) are as common as maps (6) and have valid literal repros, so 1.3 ships
structs and parks maps + `FALS003` (nil-map write) together for a later slice.

<details><summary>original plan</summary>
This is the bulk of the work and where real programs live.
- **Structs**: enumerate field tuples (bounded, reuse scalar domains per field);
  interpreter already needs a struct value in `fval` (add `k:KStruct, fields
  map[string]fval`). Field-access + field-assign + nil-struct traps.
- **Maps**: `fval{k:KMap}`; enumerate small key/value sets; `m[k]` on absent key
  (MFL returns zero — confirm semantics, no trap) vs `has(m,k)`; nil-map traps.
- **Interprocedural**: the spike stubs user calls to `0`. Two options — pick
  **(a)** for Phase 1:
  - (a) **bounded inlining**: interpret callee bodies directly (recursion-depth
    cap → `unknown`). Simple, precise, matches monomorphized instances already in
    the checker (`callInst`/`instFn`). Risk: exponential on deep call graphs →
    rely on the cost cap.
  - (b) summaries (precondition inference) — deferred; more powerful, much harder.
- Corpus: run over the example/backend corpus; **bucket every `unknown`** by
  cause (unsupported node, cap hit, FFI) so we never mistake "didn't check" for
  "clean". FFI calls are opaque → `unknown` by construction (documented).
</details>

### Slice 1.4 — repro hardening + honesty surface — ✅ DONE

Implemented:
- **Per-function verdict envelope**: the `--json` report gains `functions:[{fn,
  verdict, tried}]` (verdict ∈ counterexample|clean|unknown|skipped, aggregated by
  source name with worst-instance-wins) and `bounds:{sliceLenMax, intDomain,
  callDepth}`. It **never** emits `proved` — a `clean` verdict is explicitly "no
  bug within these bounds". (Dropped the planned `elapsed_us` field: timing is
  nondeterministic and would break the golden/reproducibility tests; agents get
  `tried` + `bounds` instead, which is what actually qualifies the verdict.)
- **`--strict`** (named `--strict`, not `--falsify-strict`): exits non-zero on any
  counterexample, advisory (exit 0) by default. Only counterexamples gate —
  `unknown`/`skipped` never do (absence of a bug is not a proof of safety).
- **Repro golden gate**: the repro-builds-and-panics-under-`--safe` assertion is a
  permanent test across `TestFalsifyFinds`, `TestFalsifyStructsAndCalls`,
  `TestFalsifyCmdRepro` (the shell corpus gate `verify-falsify.sh` lands in 1.5).
- Docs (`check-json.md` envelope block + interproc/struct note) and `guide.go`
  (`--strict`, `functions`/`bounds`) updated. Tests: `TestFalsifyCmdJSON`
  (functions/bounds/no-`proved`), `TestFalsifyCmdStrict` (exit codes via exec).

<details><summary>original plan</summary>
- Repro determinism: literal-args form only (no globals/time); confirm each
  emitted repro builds+panics under `--safe` in a golden test (the spike already
  does this — make it a permanent gate).
- `machin falsify --json` verdict envelope per function: `{fn, verdict:
  "counterexample"|"unknown", tried, elapsed_us, bound}` so an agent sees the
  coverage, not just findings. Never emit `proved` in Phase 1.
- Optional `--falsify-strict` for CI (fail on any counterexample), off by default.
</details>

### Slice 1.5 — corpus guard + close-out — ✅ DONE

- **`verify-falsify.sh`** (15/15): a *behavioral* gate (falsify isn't self-hosted
  yet, so it drives the real binary, not an oracle diff). 6 planted bugs — each
  must be found AND its `--repro` must panic under `--safe`; 6 adversarial
  correct-but-tricky negatives (guarded average/division/index, `len`-bounded sum,
  struct-copy value-semantics, recursion) — each must stay clean; a corpus sweep
  that proves the two real latent bugs in `multi_return.mfl` and buckets every
  `unknown` (printed, never silent).
- `go test .` green; **race/cgen gates untouched** — the diff is only Go +
  docs (`falsify.go`, `falsifycmd.go`, one `check.go` hook, one `main.go` dispatch
  line, `guide.go`, `docs/`), zero `selfhost/`, `racecheck.go`, or `codegen.go`.
- Memory (`machin-falsify.md`) written. `BOOTSTRAP.md` intentionally NOT touched —
  it documents the self-hosting oracle-diff bootstrap, and falsify is a Go-only
  reference until the Phase 2 port.

---

## Phase 1: COMPLETE

The Falsifier is a real, shipped-quality `machin` capability on branch
`falsify-spike` (unmerged, awaiting the merge gate). It finds `FALS001`/`FALS002`
bugs across arithmetic, calls, and struct fields; surfaces them advisorily in
`machin check`; exposes `machin falsify` with an honest verdict envelope and
runtime-confirmed repros; and is guarded by Go tests + `verify-falsify.sh`.
Deferred, documented: maps + `FALS003`. Next: Phase 2 (self-host port).

## Phase 2 — self-host port: ✅ COMPLETE

Ported `falsify.go` → `selfhost/falsify.src` over the self-hosted IR (`nodes[]` +
`g_insts`), oracle-diffed via a `falsifytest --program` hex dumper exactly like
`racetest`/`racecheck.src`. **machin-in-machin now both compiles AND falsifies,
byte-identically.** Driver: `selfhost/falsifymain.src`. Gate:
`selfhost/verify-falsify.sh` (38/38) + a broad corpus sweep (47/47 example files
where both pipelines parse+check).

The one architectural problem: **MFL has no panic/recover and no closures**, so the
Go interpreter's `panic(fviol)`/`panic(funknown)` unwinding and `ctrl` returns are
replaced by threaded status globals (`g_fstatus`, `g_freturned`/`g_fbreak`/
`g_fcontinue`), and interprocedural inlining saves/restores the global env around
each call instead of using call frames. The value model is an `FV` struct (int /
`[]int` / float / string / bool / struct) — MFL has no `[][]FV`, so slice and struct
domains are enumerated as flat `[]FV` / decoded on-the-fly from the enumeration index.

Slices: 2.1 scalar-int spike (proved status-threading + byte-exact render/hex),
2.2 `[]int` + `FALS001` + len/while/for-range, 2.3 float + string, 2.4 struct +
bool (bool renders `true`/`false`, value-semantics clone), 2.5 interprocedural
inlining (recursion-capped) → **full parity**. The 9 corpus files that differ are
pre-existing shared self-hosted pipeline gaps (multi-line parse, closures,
variadic, globals) — the already-shipped race pass diverges on them identically.

## Phase 3 — user contracts: ✅ COMPLETE

Declarative **design-by-contract**: a function carries trailing `requires <expr>` /
`ensures <expr>` clauses on its signature (after the returns, before the body).
This is where falsify stops checking only free properties and starts checking
**author intent**.

- **`requires`** (over params) is a **precondition that filters the input domain** —
  an input failing it is the caller's fault, skipped; it *suppresses* a would-be
  `FALS001`/`FALS002` that only occurs on invalid input.
- **`ensures`** (over params + named returns) is a **postcondition** checked after
  the body; an input satisfying every `requires` that makes an `ensures` false is a
  **`FALS010`**, carrying the offending clause.

Both the Go reference (parser clause on `FuncDecl.Requires`/`Ensures`; `runOne`
filter + `evalPred`) and the self-host port (`selfhost/parse.src` encodes contracts
into `K_FUNC.kids2`; `selfhost/falsify.src` `f_eval_pred` + `f_one`) are done and
**oracle-diffed byte-identically** — `selfhost/verify-falsify.sh` 45/45, corpus
47/47. Predicates are tri-state (false/true/inconclusive): a non-bool / trapping /
unmodeled predicate is inconclusive, never a false FALS010.

Chosen syntax: **declarative clauses** (over inline assert/assume) — user decision.
Known limitation: a FALS010 is a spec violation, not a runtime trap, so its
`--repro` runs the function with the failing input but does not panic under
`--safe` (an assertion-style repro is a possible follow-up); `invariant` (loop
invariants) not yet implemented.

## After Phase 3 (outline, not committed)

- **Phase 4 — the `proved(k≤N)` verdict**: symbolic/path-bounded upgrade (still
  pure-MFL, no SMT dependency — a bounded model check over the typed IR). Only
  now may output claim proof, always bound-labelled and honest.
- **Blog** (blog.intrane.fr via ~/ai/superlandings): "The compiler that hands you
  the murder weapon" — falsification as the default agent workflow.

## Risks & mitigations

| Risk | Mitigation |
|---|---|
| Bounded search misses deep bugs → false confidence | Never claim `proved` in P1; verdict envelope shows `tried`/`bound`; bucket `unknown`. |
| False positives on correct code erode trust | `--safe` repro must panic or the finding is dropped (self-checking). Adversarial negative corpus in 1.5. |
| Interprocedural blow-up | Cost cap + recursion-depth cap → `unknown`, logged. |
| No source columns | Reuse per-decl line + rendered expr for v1; column-precise positions a stretch goal needing lexer/parser position tracking. |
| FFI opacity | Documented `unknown` by construction; not a bug. |
| Scope creep into a verifier | Hard gate: P1 is falsify-only, advisory, non-build-failing. `proved` is Phase 4. |

## Definition of done (Phase 1)

- `machin falsify <file>` finds the planted-bug classes with runtime-confirmed
  repros and no false positives on the negative corpus.
- `check --json` surfaces `phase:"falsify"` diagnostics; `build` is never failed
  by them unless `--falsify-strict`.
- Every `unknown` is bucketed; no silent under-reporting.
- `verify-falsify.sh` + `falsify_test.go` green; existing gates untouched;
  released as a minor version with `guide.go`/docs updated.
