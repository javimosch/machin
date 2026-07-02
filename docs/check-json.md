# `machin check --json` — agent-native diagnostics

## Why
machin is machine-first: an **agent** writes and edits MFL, not a human in an editor. The
machine-first analog of "IDE tooling" is therefore not a language server (which renders for
a human eyeball) but **fast, structured feedback an agent can act on programmatically.**

`machin guide` already gives the agent the language *surface* (what it can write, as bulk
JSON). The missing half is the language *verdict*: **what's wrong, as data.** Today an agent
runs `machin build` — which does full codegen **and invokes `cc`** (slow) — and then
regex-scrapes human-readable error prose off stderr. `machin check --json` closes the agent's
**write → check → fix** loop: no codegen, no `cc`, sub-second, and errors as objects.

## CLI surface
```
machin check [--json] [--symbols] <file.src|.mfl> [more.src ...]
machin check [--json] --stdin              # read source from stdin (no temp file)
```
- Runs **lex → parse → typecheck only.** No codegen, no `cc`. This is the speed win —
  checking is milliseconds; a full `build` pays seconds for `cc`.
- Accepts loose `.src` (encoded internally) or canonical `.mfl`, one or many files
  (concatenated like `encode`/`build`), or `--stdin` (an agent pipes the buffer it just
  wrote and reads the verdict back — zero filesystem).
- **Exit code**: `0` iff no errors; non-zero otherwise. So an agent can gate on the exit
  code *and* parse the JSON — belt and suspenders.
- Without `--json`: the existing human-readable text (unchanged default).

## Output schema (`--json`)
One JSON object on stdout (never streamed/partial — trivially parseable):

```json
{
  "ok": false,
  "files": ["app.src"],
  "errorCount": 2,
  "diagnostics": [
    {
      "severity": "error",
      "phase": "typecheck",
      "code": "type-mismatch",
      "message": "type mismatch: bool vs num",
      "decl": "score_of",
      "line": 42,
      "col": 14,
      "snippet": "s = is_ready(x) + 1"
    },
    {
      "severity": "error",
      "phase": "parse",
      "code": "unexpected-token",
      "message": "expected ) , got {",
      "decl": "main",
      "line": 88,
      "col": 20,
      "snippet": "if compute(a b) { ... }"
    }
  ]
}
```

### Diagnostic fields
| field | type | notes |
|---|---|---|
| `severity` | `"error"` \| `"warning"` | only `"error"` in v1 (machin has no warnings yet) |
| `phase` | `"lex"` \| `"parse"` \| `"typecheck"` \| `"race"` | where it was caught (`race` = the concurrency analysis, below) |
| `code` | string | **stable machine code** — the agent branches on this, never on `message`. Enumerated below. |
| `message` | string | the human-readable detail (the agent may still read it, but shouldn't pattern-match it) |
| `decl` | string | the declaration (function / global / type name) the error is in — the natural fix unit (see below) |
| `line` | int | 1-based; best-effort in v1 (see position mapping) |
| `col` | int | 1-based; best-effort in v1, precise in v2 |
| `endLine`,`endCol` | int | optional range, v2 |
| `snippet` | string | the offending source fragment, to help the agent locate |

### `code` enumeration (initial, stable)
`parse-unexpected-token`, `parse-unterminated-string`, `parse-unbalanced-braces`,
`type-mismatch`, `undefined-name`, `undefined-field`, `arity-mismatch`,
`not-callable`, `no-main`, `unsupported-construct`. Concurrency codes (phase `race`):
`RACE001`, `RACE002`, `RACE004` (below). New codes are additive; existing codes never
change meaning.

## Concurrency: inferred data-race diagnostics (phase `race`)
After a clean typecheck, `check` runs an **inferred data-race analysis** — the guarantee
Rust gives via `Send`/`Sync`, but with **zero annotations**: it infers which heap
locations are shared *and* concurrently accessed across goroutine boundaries. Every
finding names a **counterexample** in `message` (who accesses the location, concurrently,
and how). The analysis is **sound** (it never misses a real race on the surface it covers)
and **conservative** (it may over-report rather than stay silent).

| code | meaning |
|---|---|
| `RACE001` | write/write — ≥2 concurrent writers of the same shared location (slice/map element, struct-with-slice field, or a package global — even a scalar one) |
| `RACE002` | read/write — a concurrent read and write of the same shared location |
| `RACE004` | use-after-move — a value used after it was sent on a channel (ownership transferred to the receiver) |

What is "shared" is **reachability-based**: parameters copy at the goroutine boundary, so
a value races only when its type reaches a slice/map (a scalar struct field is private; a
slice *field* keeps its shared backing). Package globals are a single shared cell, so they
race unconditionally. Closures reach a goroutine only as a func-arg to a `go`-spawned
function; their captured slices are shared by-reference and analyzed as such.

Happens-before is respected: an access **before** a goroutine is spawned, or **after** a
channel-join barrier (a goroutine whose last statement signals a channel the spawner then
receives), is ordered — not a race. Build enforcement: `machin build|run --race-safe`
refuses to compile a program with an inferred race (plain `build` is unaffected).

### Top-level fields
`ok` (bool), `files` (the inputs), `errorCount` (int), `diagnostics` (array, **stable
order**: source order, phase-then-position).

## The key design choice: `decl`-level granularity is enough
MFL is **one canonical declaration per line** and agents edit **function-by-function**. So
the naturally-actionable unit is not a character range but the *declaration*: *"`type-mismatch`
in `score_of`: bool vs num."* That's already enough for an agent to regenerate or patch that
one function. This is why v1 is tractable and still high-value — precise column ranges are a
polish, not a prerequisite.

## Implementation
- **Reuses the existing checker** (`Check()` in Go; the same engine is already ported to
  `selfhost/check.src` + `checkgen.src`, so a self-hosted `machin check` is free later). No
  new analysis — this is a **reporting/serialization layer** over the checker's existing
  errors.
- **Threading positions** is the one real task. AST nodes already carry the lexer `pos`; the
  checker's errors must be tagged with the offending node so the reporter can resolve
  `decl` + `line`/`col`. v1: collect `{phase, code, message, decl, node}` at each existing
  `g_err`/`g_terr`/`g_unsupported` site instead of a bare string.
- **Position mapping caveat (honest):** `encode` collapses multi-line `.src` into one-line
  `.mfl`, so precise `.src` line/col needs a **source map** from `encode` (original line/col
  per token). v1 ships **`decl` + `.mfl` line + best-effort col**; v2 adds the source map for
  exact `.src` ranges. `decl`-level is the useful floor.
- **Multiple errors, not first-only:** the checker should continue past the first error where
  safe (per-declaration) so one `check` returns the full batch — an agent fixes them in one
  pass, not one-per-round-trip.

## Non-goals
- No LSP protocol, no editor integration, no incremental/streaming server. This is a **CLI
  that returns data**, aligned with how agents already invoke machin (`guide`, `encode`,
  `build`).
- Not a linter/style tool (no unused-var, formatting opinions) — those are separate.

## How it fits
`machin guide` (surface) + `machin check --json` (verdict) are the two halves of the
machine-first "IDE": one tells the agent *what it can write*, the other *what's wrong with
what it wrote* — both as bulk, deterministic JSON, no human in the loop. It also speeds the
tool's *own* users: any agent orchestrating machin builds (yours included) gets a tight,
programmatic edit loop instead of scraping stderr.
