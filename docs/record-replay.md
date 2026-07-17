# Deterministic record/replay

`machin run --record <trace> <file>` captures a run; `machin replay <trace>`
re-executes it **byte-for-byte** — feeding the recorded goroutine schedule and I/O
instead of the real clock, stdin, and thread scheduler. A crash becomes a
**mailable artifact**: the trace file reproduces it anywhere, and an agent reads
the failure instead of trying to reproduce it.

## Why it's sound (and cheap)

`rr` and friends have to record every memory access, which makes them x86-only and
**unsound under data races**. machin doesn't: it proves every program
[data-race-free](fearless-concurrency-without-send-sync), so the *only*
inter-goroutine nondeterminism is the **order channel operations complete**. Record
that order — a short sequence of goroutine ids — plus the process's I/O returns,
and the run is fully determined. No memory logging, no architecture lock-in.

## Usage

```bash
machin run --record trace.mflrr app.mfl    # capture a run
machin replay trace.mflrr                  # re-execute it (program path is in the trace)
machin replay trace.mflrr --verify         # + certify the replay stayed faithful
machin replay trace.mflrr --json           # a crash -> a structured causal report
```

The trace embeds the program path and the `--safe` build flag, so `machin replay`
rebuilds and re-runs without you re-naming the source — and a crash the recorded
`--safe` run caught reproduces.

## What is captured

| Source of nondeterminism | Captured how |
|---|---|
| Goroutine scheduling | the order channel ops (send/recv/close) complete, as goroutine ids |
| Concurrent stdout | each `print`/`println` is gated too, so output interleaving replays |
| Time | `now`/`now_ms` results are logged and fed back |
| Stdin | `read_stdin` is logged; replay never blocks on the real stdin |
| Randomness | `rand_bytes` draws are logged; a crypto program replays **faithfully**, not best-effort |
| Files | `read_file`/`read_file_bytes` contents are logged, so replay reproduces even after the files are gone — the trace is self-contained ("the crash you can mail") |

Goroutine ids are **parent-relative paths** (`0`, `0.1`, `0.1.2`) assigned in the
spawning goroutine's program order, so they're stable across record and replay even
under concurrent nested spawns.

## The determinism boundary (honest by design)

Replay is faithful only inside the boundary machin controls. A program that uses
**FFI (`extern`)** leaves it — the FFI call's result is uncaptured. Such a trace
is flagged **`best-effort`** in its header; `machin replay` prints a warning, and
`--verify` will never certify it `FAITHFUL`. The tool would rather refuse than
present a possibly-divergent replay as the real thing.

`select` **is** inside the boundary: its poll is gated. The chosen case index is
recorded and, on replay, forced with a blocking op that waits its turn in the
replayed schedule — so which case fired (including how often `default` fired) is
reproduced exactly. A select-using program records a `faithful` trace.

`--verify` is a self-check: a faithful replay consumes exactly the recorded
schedule and never underruns the I/O log — anything else reports `DIVERGED`.

## Crash → causal report

When the recorded run panicked, `machin replay --json` reproduces it and emits:

```json
{
  "panic": "index out of range [1] with length 1",
  "goroutine": "0",
  "scheduleOp": 3,
  "scheduleTotal": 3,
  "causalChain": ["0.2", "0.1", "0"]
}
```

`causalChain` is the sequence of channel-op goroutines that led to the crash — the
story an agent reads: goroutine `0.2` acted first, then `0.1`, then `0` (main),
which panicked.

## Scope

In-process determinism only. Cross-process coordination between goroutines is out
of scope. The trace format is versioned (`MFLRR 1`). Randomness (`rand_bytes`) and
file reads (`read_file`/`read_file_bytes`) are now captured, and `select` is gated,
so those programs replay faithfully and self-contained. Not yet covered (same
pattern, follow-ons): socket I/O, and a value-query replay debugger
(`--at <site> --print <var>`). The runtime is self-hosted (the self-hosted compiler
emits byte-identical instrumentation).
