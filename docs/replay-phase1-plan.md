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

### Slice 1.1 — Robust gid + full channel coverage — ✅ DONE

- **Deterministic gid** via a parent-relative PATH (`0`, `0.1`, `0.1.2`), assigned
  in the spawning goroutine's program order (thread-local `mfl_gid_path` +
  `mfl_spawn_ctr`, passed to the child as `(parent path, spawn index)`). Ids are
  now identical across record/replay even under concurrent nested spawns — proven
  on a 2-level spawn tree that produced paths `0.1.1`/`0.2.1`/… and replayed
  deterministically while plain runs gave 6 orderings. Replaces the spike's racy
  global counter. Trace is now a list of path strings.
- **`close`** instrumented (an observable, ordered op — a recv after close gets
  "not ok", so its position matters). Range-over-channel and `recv` already ride
  the `recv2` hook.
- **Learning / bug fixed:** the drain-after-close `recv2` (returns "not ok")
  must ALSO `rr_mark` — it gates a turn, so if it doesn't record one, record and
  replay disagree on the op count and replay hangs. Caught by the close/range
  fixture. `verify-replay.sh` now 6/6 (flat race, crafted-trace control, nested
  spawns, close+range), compile-once for speed.

**Deferred to Slice 1.3 (honesty):** `select` (`mfl_chan_tryrecv2`). Its
poll-loop replay is a distinct, harder problem — a non-ready poll must not consume
a turn, and a busy-poll select can't just block-until-turn without deadlock. Left
uninstrumented for now (so it never hangs), and 1.3's boundary work will flag a
`select`-using program `best-effort` until a dedicated select-determinism slice
lands. machin channels are unbounded-async (send never blocks), so buffered vs
unbuffered is not a distinct case.

### Slice 1.2 — The I/O log — ✅ DONE (time + stdin; random/file/net follow-on)
- **Versioned tagged trace format** (`MFLRR 1` header; `S <path>` schedule lines;
  `I <path> <hex>` I/O lines). Each goroutine replays its OWN I/O queue in order,
  so the I/O log needs no global ordering — the schedule already orders everything
  observable across goroutines.
- **Interposed** the nondeterministic I/O builtins at the runtime boundary:
  `now`/`now_ms` (time) and `read_stdin`. Under `--record` the real call runs and
  its result is hex-logged; under `--replay` the logged value is returned and the
  real call is **skipped** (so replay never blocks on stdin / never reads the
  wall clock). Per-goroutine queues keyed by gid path.
- Proven (`verify-replay.sh` 11/11): recorded time replays identically seconds
  later (plain differs); recorded stdin replays despite different/empty real
  stdin without blocking; and a combined fixture where workers **race for a
  channel AND each record a timestamp** replays schedule + per-worker I/O
  together.
- **Follow-on (same pattern, this phase):** `rand_bytes` (crypto-gated, returns a
  slice — hex-log its bytes like stdin), file reads, and socket `recv`.

### Slice 1.3 — The determinism boundary + honest refusal — ✅ DONE
- **Boundary detection (compile-time).** codegen emits `mfl_rr_prog_boundary()`
  returning 1 when the program uses **FFI (`extern`)** or **`select`** — the two
  ways a program leaves the boundary machin can control. Conservative + honest: a
  program that *can* call FFI is best-effort.
- **Honest trace header.** `--record` writes `boundary faithful|best-effort` into
  the trace. `--replay` reads it and, on a best-effort trace, prints a `warning:`
  to stderr — never presents a possibly-divergent replay as faithful.
- **`--verify`** (`MFL_RR_VERIFY`): an honest self-check at replay end — a faithful
  replay consumes exactly the recorded schedule (`pos == n`) and never underruns
  the I/O log; anything else (or a best-effort trace) reports `DIVERGED`. Wired to
  `machin run --verify`.
- Proven (`verify-replay.sh` 17/17): a pure program → `faithful` + `--verify`
  certifies FAITHFUL; an FFI program → `best-effort`, replay warns, `--verify`
  refuses to certify (DIVERGED); a `select` program → `best-effort`. `guide.go`
  updated (run verb documents `--record`/`--replay`/`--verify` + the boundary).
- (The dedicated `machin replay <trace>` command — loading a trace + re-running
  without re-specifying the source — is Slice 1.4; here replay rides
  `machin run --replay`.)

### Slice 1.4 — The payoff: `machin replay` + the causal report — ✅ DONE
- **`machin replay <trace>`** — the headline verb. The trace embeds its `program`
  path + `safe` build flag (written at record time from `MFL_RR_SRC`/`MFL_RR_SAFE`),
  so you replay *without re-naming the source* and a crash the recorded `--safe`
  run caught reproduces (replay rebuilds with the same `--safe`). Proven: replays
  a run 5/5 identically; `--verify` certifies FAITHFUL.
- **Crash → causal artifact.** `mfl_panic` is enriched: when record/replay is
  active it names the panicking goroutine + schedule position; under `--json` it
  emits a structured causal report — `{panic, goroutine, scheduleOp,
  scheduleTotal, causalChain:[the channel-op goroutine-ids that led to the
  crash]}`. Proven on a schedule-caused index-out-of-range: the report pinpoints
  goroutine `0` panicking after 3 ops with `causalChain ["0.2","0.1","0"]` (worker
  `0.2` sent first → main got the bad index), and the crash replays deterministically.
- **Learning:** replay must rebuild with the recorded program's `--safe` setting,
  or an index-OOB/div-zero crash silently *doesn't* reproduce (reads garbage
  instead of panicking) — so the `safe` flag is recorded in the trace.
- `verify-replay.sh` 20/20; `guide.go` gains the `replay` verb. (Full
  "query any variable at iteration N" replay-debugger remains a follow-up.)

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
