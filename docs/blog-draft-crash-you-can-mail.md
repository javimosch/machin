# The crash you can mail

*A concurrency bug that only happens on the server, reproduced byte-for-byte on your laptop — because the language already proved the program race-free.*

---

The worst bug report is "it crashed once, in production, and I can't reproduce it." Concurrency makes that the *normal* case. A goroutine interleaving that happens one run in a thousand, on a loaded box, with a particular API response — you get a stack trace and a shrug. "Works on my machine" isn't a joke, it's the default.

The state of the art for fixing this is `rr`, Mozilla's record/replay debugger. It records **every memory access** so it can replay a run deterministically. It's brilliant and it's heavy: x86-only, and — crucially — **unsound under data races**, because the thing it's trying to pin down is exactly the thing races make nondeterministic.

I built record/replay into [machin](https://github.com/javimosch/machin) too. But machin gets to cheat, and the cheat is the whole point.

## The cheat: it's already race-free

Machin infers [data-race freedom with no annotations](https://blog.intrane.fr/fearless-concurrency-without-send-sync) — the compiler proves, before the program runs, that no two goroutines touch shared memory without synchronizing. Sit with what that buys you for replay:

**If the program has no data races, the *only* source of inter-goroutine nondeterminism is the order in which channel operations complete.**

That's it. Not memory accesses — there are no unsynchronized ones. Just the schedule: which goroutine's send or receive landed first. So machin doesn't record millions of memory reads like `rr` has to. It records a short list of goroutine ids — the order channel ops fired — plus the I/O the program pulled in from the outside world. A recording that's *sound* precisely because the race checker did its job first.

## Show me

Here's a program with a real interleaving race — three workers, whoever's scheduled first wins:

```go
func feed(ch, base, dly) { i := 0  for i < 5 { sleep(dly)  ch <- base + i  i = i + 1 } }
func main() {
    a := make(chan int)  b := make(chan int)
    go feed(a, 100, 2)  go feed(b, 200, 3)
    got := 0  order := ""
    for got < 10 {
        select {
        case x := <-a: order = order + "a" + str(x) + " "  got = got + 1
        case y := <-b: order = order + "b" + str(y) + " "  got = got + 1
        }
    }
    println(order)
}
```

Run it a few times and the output genuinely varies — the `a`/`b` interleaving is a coin flip on timing. Now record one run and replay it:

```
$ machin run --record run.mflrr flaky.mfl
a100 a101 b200 a102 a103 a104 b201 b202 b203 b204

$ machin replay run.mflrr          # again, and again, and again
a100 a101 b200 a102 a103 a104 b201 b202 b203 b204

$ machin replay --verify run.mflrr
replay-verify: schedule 21/21 ops, io-underrun=no, boundary=faithful -> FAITHFUL
```

Byte-for-byte, every time — including which `select` case fired on each of the ten iterations, which is pure scheduling luck. The trace is 21 schedule entries and a handful of I/O values. Mail it to a colleague and they reproduce your exact run on their machine, no server, no load, no luck required.

## Self-contained, on purpose

"No server required" is a literal claim. A recorded run captures not just the schedule but everything the program read from outside itself: the clock, stdin, random bytes, file contents, raw sockets, and HTTP/TLS/WebSocket responses. So the trace stands alone.

Record a program that reads a file, then **delete the file** and replay:

```
$ machin run --record t.mflrr reader.mfl     # reads /etc/secret → "payload-42"
$ rm /etc/secret
$ machin replay t.mflrr
payload-42                                    # the bytes were in the trace
```

Record an API call, then **kill the server** and replay:

```
$ machin run --record t.mflrr client.mfl      # GET localhost:8080 → 200 "…"
$ kill %server
$ machin replay t.mflrr
200:PAYLOAD                                    # the response was in the trace
```

Same story for `rand_bytes` (a crypto program replays with the exact bytes it drew) and raw TCP (replays while another process squats the port, because it never opens a socket). *The crash you can mail* is the design goal, and the trace is the envelope.

## Honesty about the boundary

Here's the part I care most about, and it's the same discipline machin's [bounded bug-finder](https://github.com/javimosch/machin) follows: **never claim a guarantee you can't back.**

Replay is faithful only inside the boundary machin controls. One thing steps outside it: **FFI**. The moment your program calls into C through `extern`, the result of that call is opaque — machin didn't compute it and can't reproduce it. So a trace from an FFI-using program is stamped `best-effort` in its header, `machin replay` prints a warning, and `--verify` will **never** certify it `FAITHFUL`. The tool refuses to present a maybe-divergent replay as the real thing.

Everything else that varies between runs is captured, so it stays inside the boundary: the channel schedule, concurrent `print` interleaving, time, stdin, randomness, files, sockets, and the whole HTTP/TLS/WebSocket layer. `select` looks like it should escape — its poll is timing-dependent — but it's gated: machin records which case fired and, on replay, forces exactly that one with a blocking op that waits its turn in the schedule. Even *how many times a `default` branch fired* — pure timing — comes back identical.

`--verify` is the honesty check made mechanical: a faithful replay consumes exactly the recorded schedule and never runs short on the I/O log. Anything else reports `DIVERGED`, out loud.

## A crash becomes an artifact you can read

Reproducing a crash is table stakes. The real payoff is that a *reproducible* crash can be turned into something an agent reads instead of re-runs.

Point `--json` at a trace that panicked and you get a causal report — the panicking goroutine, how far into the schedule it got, and the chain of channel-op goroutines that led there:

```json
{ "panic": "index out of range [12] with length 2",
  "goroutine": "0", "scheduleOp": 4, "scheduleTotal": 4,
  "causalChain": ["0.2", "0.1", "0"] }
```

And because replay is deterministic, you can *watch a variable* on the way to the crash. `--print` re-runs the exact recorded execution and prints a variable's value history:

```
$ machin replay crash.mflrr --print idx
probe 0 idx = 0 @op0
probe 0 idx = 5 @op3
probe 0 idx = 12 @op4
panic: index out of range [12] with length 2
  (replay: goroutine 0, schedule op 4/4)
```

`idx` climbed 0 → 5 → 12, and 12 walked off the end. The last line before the panic is the state at the crash — no debugger session, no breakpoints, no "add a print and hope it happens again." The run is frozen; you just query it.

## The machine can consume it

The compiler that emits all of this instrumentation is itself written in machin and compiles itself — the record/replay runtime is byte-for-byte identical whether the Go-hosted or the self-hosted compiler built your binary. That's not a flex; it's the same soundness argument one level up.

This is the third piece of a trilogy, and they rhyme. Machin proves your concurrent program is **race-free**. It hands you the **exact input** that breaks a function, within bounds. And now it makes a crash **reproducible and portable** — a JSON artifact with the schedule, the inputs, and the causal chain baked in.

The connecting thesis is the same one behind the whole language: a crash report is only as useful as what can *consume* it. A human consumes a stack trace slowly and a heisenbug not at all. An agent — the thing writing and fixing this code — consumes a faithful, self-contained, causally-annotated replay directly. It doesn't need to reproduce the bug. It's holding the run.

So it's not "works on my machine" anymore. It's the machine, mailed.

---

*machin is open source at [github.com/javimosch/machin](https://github.com/javimosch/machin). It's built by [Javier Arancibia](https://intrane.fr) — the same engineering that goes into [intrane.fr](https://intrane.fr).*
