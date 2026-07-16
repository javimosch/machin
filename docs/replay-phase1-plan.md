# Sound record/replay — Phase 1 plan

*A machin binary you can run with `--record`; a crash becomes a mailable trace;
`machin replay` re-executes it byte-for-byte and hands an agent the causal chain.
Phase 0 proved the crux (deterministic channel-schedule replay). This is the plan
to make it a real, shipped capability.*

## Thesis

Debugging is the loop an agent is worst at — it can't sit in gdb, and its default
move (add a print, re-run, hope the bug reappears) burns turns and fails when the
bug is nondeterministic. Record/replay collapses that: the failing run is captured
once and replayed deterministically forever, and the trace is a **mailable
artifact** — a production crash arrives as a file the compiler re-executes. It
composes with the arc so far into one story: *machin binaries carry their own
evidence — race-free, bug-free within bounds, and reproducible.*

## What Phase 0 proved (the baseline)

Because machin proves every program **data-race-free**, the only inter-goroutine
nondeterminism is the *order channel operations complete*. The spike records that
order as a sequence of goroutine ids and, on replay, gates each channel op to fire
in the recorded order — **reproducing the run without recording a single memory
access** (which is what makes `rr` unsound under races and x86-only). Verified
(`verify-replay.sh` 4/4): plain runs vary; `--replay` reproduces the recording
10/10; a crafted trace fully controls the schedule. Reference: the C runtime hooks
in `codegen.go` (`mfl_gid` / `mfl_rr_*` around `mfl_chan_send`/`recv2`) +
`--record`/`--replay` in `cmdRun`, on branch `replay-spike`.

## The one design decision that governs everything

**Replay is sound only inside the *determinism boundary* machin controls: the
scheduler (channels) + the no-cgo I/O layer (time, random, stdin, file, net). An
FFI/extern call crosses that boundary — its result is nondeterministic and
uncaptured — so a program that calls non-pure FFI cannot be faithfully replayed.**

The honesty rule (the analog of the Falsifier's "never claim a proof you can't
back"): the tooling must **detect** when a program leaves the boundary and refuse
to claim a faithful replay — mark the trace `best-effort`, never silently produce a
replay that diverges. In-process determinism only: filesystem/cross-process
coordination between goroutines is out of scope and documented.

Consequences:
- Fully replayable: pure-MFL + the whole no-cgo backend stack (HTTP, the pure-MFL
  Postgres/Redis/Mongo drivers, sessions) — machin's actual server surface.
- Best-effort / refused: raylib games and other FFI-heavy programs.

## Trace format

A single versioned file with two logically-separate streams, so each goroutine
replays independently:

- **Schedule**: the sequence of goroutine ids at channel-op completions (Phase 0).
- **I/O log**: keyed by `(gid, per-gid-io-seq)` → the exact bytes the call
  returned. Each goroutine replays *its own* I/O sequence, so no global I/O
  ordering is needed — the schedule (channels) already orders everything that's
  observable across goroutines.

Header: format version, a hash of the program (so a trace can't be replayed
against a different binary), and a `boundary` flag (`faithful` | `best-effort`).

## Slices

Record/replay is a **runtime** feature (C emitted by `codegen.go`), so the "Go
reference" here is the Go-compiler-emitted runtime; the self-host port (Phase 2)
is making the self-hosted `cgen.src` emit the same runtime, oracle-diffed.

### Slice 1.1 — Robust gid + full channel coverage
- **Deterministic gid regardless of spawn timing.** The spike assumes spawns
  happen in a deterministic order (main spawns all). Fix: derive a gid from the
  *parent* gid + a per-parent spawn counter (a stable tree path), so ids are
  identical across runs even when goroutines spawn goroutines concurrently.
- **Instrument every channel op**, not just `send`/`recv2`: `select`
  (`mfl_chan_tryrecv2` — the poll loop needs care, a non-ready poll must NOT
  consume a turn), range-over-channel, `close`. Unbuffered vs buffered channels.
- Gate: nested-spawn, `select`, buffered, and close programs replay
  deterministically; extend `verify-replay.sh`.

### Slice 1.2 — The I/O log
- Interpose the nondeterministic I/O builtins at the runtime boundary: `now`/time,
  `random`, `read_stdin`, file reads, socket `recv`. Under `--record`, log the
  returned bytes at `(gid, io_seq++)`; under `--replay`, return the logged bytes
  and skip the real call.
- Implement the versioned trace format (schedule + I/O + header).
- Gate: a program using time + random + stdin is bit-identical under replay while
  plain runs differ.

### Slice 1.3 — The determinism boundary + honest refusal
- Detect boundary exits: an `extern`/FFI call under `--record` sets the trace's
  `boundary` flag to `best-effort` (and, optionally, records a warning naming the
  call). A `faithful` claim requires the program to have stayed pure-MFL + no-cgo
  I/O for the whole run.
- `machin replay` reports the boundary status up front and, on a `best-effort`
  trace, says so — never presents a possibly-divergent replay as faithful.
- Optional `--verify`: re-hash the replay's output stream against a recorded hash
  and report match/divergence (a self-check that the replay reproduced the run).
- Document the boundary in `docs/` + `guide.go`.

### Slice 1.4 — The payoff: `machin replay` + the causal report
- `machin replay <trace>` re-executes deterministically (the headline command).
- **Crash → causal artifact.** Record continuously; if the recorded run panicked,
  `machin replay` re-runs to that panic and emits a structured (JSON) causal
  report: the panicking goroutine (gid + its spawn path), the source site, the
  channel-op sequence that led there, and — via the existing `--safe` trap
  machinery — the offending value. This is the agent-facing surface: a crash the
  agent *reads* instead of reproduces.
- (Scope note: a full "query any variable at iteration N" replay-debugger is a
  follow-up; the first cut is faithful re-execution + a panic causal report.)

### Slice 1.5 — Corpus, gate, close-out
- `verify-replay.sh` expanded: nested spawns, `select`, buffered, close, the I/O
  log, and a boundary/FFI-leak honesty case. Run it across a slice of the concurrent
  backend corpus (an HTTP handler using channels + the pure-MFL PG driver should
  replay `faithful`).
- `go test .` green (the runtime change is a mode-0 no-op by default — Phase 0
  already confirmed this); cgen/race gates unaffected.
- Docs (`docs/record-replay.md`) + `guide.go` (`--record`/`--replay`/`replay`
  verbs + a gotcha) + memory. Merge + release.

## After Phase 1 (outline)

- **Phase 2 — self-host port**: the self-hosted `cgen.src` emits the same
  instrumented runtime, oracle-diffed via `cgentest`, so *machin-in-machin*
  records and replays. (Runtime C is emitted by codegen, so this is a cgen change,
  not a new analysis pass.)
- **Value-query replay debugger** (`machin replay --at order.go:42 --print total`).
- **Blog** (blog.intrane.fr): "The crash you can mail" — replay as the agent
  debugging loop, sound because race-free.

## Risks & mitigations

| Risk | Mitigation |
|---|---|
| Non-deterministic gid under concurrent spawns | Parent-relative gid path (Slice 1.1) — stable regardless of thread-start timing. |
| FFI silently breaks replay | Boundary detection + honest `best-effort` flag; never present a divergent replay as faithful (Slice 1.3). |
| `select` poll consuming turns wrongly | A non-ready poll must not record/gate; only the op that actually completes takes a turn (Slice 1.1). |
| Trace replayed against wrong binary | Program hash in the header; refuse on mismatch. |
| Runtime overhead when not recording | Hooks are mode-0 no-ops (a single branch); confirmed zero behavioral change by the full suite. |
| Filesystem/cross-process coordination | Explicitly out of scope (in-process determinism only), documented. |

## Definition of done (Phase 1)

- `machin run --record` on any pure-MFL + no-cgo-I/O program, and `machin replay`
  reproduces it bit-for-bit — schedule *and* I/O — while plain runs differ.
- Boundary is enforced honestly: FFI programs are flagged `best-effort`, never a
  silent divergent replay.
- A recorded crash replays into a structured causal report an agent can read.
- `verify-replay.sh` green across the concurrency + I/O + boundary corpus; existing
  gates unaffected; shipped as a minor release with docs/guide updated.
