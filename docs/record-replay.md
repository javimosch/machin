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
| Raw sockets | `dial`/`listen`/`accept`/`read`/`read_bytes`/`write`/`socket_timeout`/`peer_addr` are logged; replay serves the whole exchange from the trace — no network, no peer, works even while another process holds the port |
| HTTP | `http_get`/`http_request`/`https_get`/`https_post` results (status + body + err) are logged; a program that crashes processing an API response replays with the exact response baked in, offline |
| TLS / WebSocket | `tls_accept`/`tls_read`/`tls_write` and `wss_open`/`wss_recv`/`wss_send` are logged like raw sockets — the encrypted exchange replays with no handshake |

Goroutine ids are **parent-relative paths** (`0`, `0.1`, `0.1.2`) assigned in the
spawning goroutine's program order, so they're stable across record and replay even
under concurrent nested spawns.

## The determinism boundary (honest by design)

Replay is faithful only inside the boundary machin controls. A program that uses
**FFI (`extern`)** leaves it — the FFI call's result is uncaptured. Everything else
that varies between runs is captured: the channel schedule, concurrent prints, time,
stdin, rand, file reads, raw sockets, and the high-level HTTP/TLS/WebSocket reads.
An FFI-using trace is flagged **`best-effort`** in its header; `machin replay` prints
a warning, and `--verify` will never certify it `FAITHFUL`. The tool would rather
refuse than present a possibly-divergent replay as the real thing.

(One known gap: interactive `read_key`/`raw_mode` TTY input is polled, not logged, so
a TUI/game replay can still diverge — a program using it is not yet flagged. Everything
in the table below is captured.)

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

## Value-query debugger (`--print`)

`machin replay <trace> --print <var>` rebuilds the recorded program with a probe
after each assignment to `<var>` and re-runs the exact recorded execution, printing
that variable's value history to stderr:

```
$ machin replay crash.mflrr --print idx
probe 0 idx = 0 @op0
probe 0 idx = 5 @op3
probe 0 idx = 12 @op4
panic: index out of range [12] with length 2
  (replay: goroutine 0, schedule op 4/4)
```

Because replay is deterministic, the sequence is exactly what the variable did in
the recorded run — the last line before a panic is its value at the crash, and
`@op<n>` correlates with the causal report's `scheduleOp`. Watch several with
`--print a,b,c`. Scalars and strings only; a normal `build`/`run` emits no probes
and pays nothing (the instrumentation exists only in a `--print` rebuild).

## Scope

In-process determinism only. Cross-process coordination between goroutines is out
of scope. The trace format is versioned (`MFLRR 1`). Rand, file reads, raw sockets,
and the HTTP/TLS/WebSocket layer are all captured, and `select` is gated, so those
programs replay faithfully and self-contained; FFI is the only best-effort boundary.
Not yet covered: interactive TTY input (`read_key`/`raw_mode`, polled not logged).
The value-query debugger (`--print <var>`, above) watches scalar/string variables.
The runtime is self-hosted (the self-hosted compiler emits byte-identical
instrumentation).
