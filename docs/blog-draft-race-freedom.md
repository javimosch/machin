# Fearless concurrency without the `Send`/`Sync` tax

*How a machine-first language infers data-race freedom — the guarantee Rust gives you, with none of the annotations.*

---

Rust made one idea famous: your program **cannot** have a data race, and the compiler proves it before it runs. That guarantee is real, and it's worth a lot. But you pay for it. You pay in `Send` and `Sync` trait bounds, in lifetimes, in the borrow checker telling you *no* until you restructure the code its way. The safety is free at runtime and expensive at authoring time — a tax the human pays, in ceremony.

I wanted the guarantee without the tax. So I built it into [machin](https://github.com/javimosch/machin).

Here's the thesis: **a machine-first language can *infer* data-race freedom, with zero annotations, because the agent writing the code and the compiler checking it are one loop.** Rust is designed for a human fighting the compiler. Machin is designed for a machine writing code the compiler then proves correct. That difference is structural — and it's exactly what makes inference tractable where Rust needed ownership types.

## The starting point: Go's concurrency, none of Go's silence

Machin (MFL — the Machine-First Language) already had real concurrency: `go`, channels, `select`, on a native pthread runtime. Like Go, it also had *zero* race analysis. This program — a textbook data race — compiled and ran without a word of complaint:

```go
var counter = 0
func incr() { counter = counter + 1 }
func main() {
    go incr()
    go incr()
    print(counter)
}
```

Two goroutines, one shared cell, no synchronization. Go's own answer is "run the race detector at runtime and hope you hit it." Rust's answer is "this won't compile unless you wrap `counter` in a `Mutex` or an atomic." Machin's new answer:

```
$ machin check counter.mfl
error: [RACE001] data race on `counter` (write/write):
  goroutine `go incr(...)` writes global `counter`;
  goroutine `go incr(...)` writes global `counter`;
  main thread reads global `counter`
```

No annotations were added to that program. The compiler *inferred* that `counter` is shared, that two goroutines write it concurrently, and that the main thread also touches it — and it handed back a counterexample naming every participant.

## What "shared" actually means

The interesting part is what the analysis has to get right to avoid crying wolf. Not everything crossing a goroutine boundary is shared. In machin, a parameter is copied at the boundary — so sharing is **reachability-based**: a value can race only if its type reaches a slice or a map (the reference types with a shared backing store).

That distinction is precise, and the checker respects it:

```go
type Bag struct { items []int  n int }

func worker(b, id) { b.items[id] = id }   // writes a SLICE field
func main() {
    bag := Bag{[]int{0, 0}, 0}
    go worker(bag, 0)
    go worker(bag, 1)
}
```

`bag` is a struct, passed by value — so writing `b.n` would be writing a private copy, no race. But `b.items` is a slice, and a slice keeps its backing array when the struct is copied. The checker walks the access path against the real type and flags exactly this:

```
error: [RACE001] data race on `bag` (write/write): ...writes `bag` ...writes `bag`
```

Write the scalar field `b.n` instead, and it's silent — because that genuinely isn't a race. Sound analysis is only worth something if it's also precise enough to stay quiet on safe code.

## The safe pattern, enforced

Go's mantra is *"don't communicate by sharing memory; share memory by communicating."* Machin makes that a checked rule via **move-on-send**: once you send a value on a channel, you've transferred ownership — the receiver may now be touching it, so you can't.

```go
func producer(ch) {
    out := []int{1, 2, 3}
    ch <- out
    out[0] = 99          // <-- used after it was handed off
}
```

```
error: [RACE004] use after move: `out` is used after it was sent on a
channel (ownership moved to the receiver)
```

Send it, then let it go. Drop the last line and it's clean — that's the idiom the whole thing is built to bless.

## Respecting happens-before (so it doesn't nag)

A sound race checker that flags every access would be useless — it'd reject correct programs. The hard, careful work is knowing what *isn't* a race. Two orderings matter, and both are honored.

**Before a spawn.** Filling a buffer and *then* starting the workers is ordered — the goroutine can't have run yet:

```go
data := []int{0, 0}
data[0] = 5          // setup, happens-before the spawn
go worker(data)      // clean
```

**After a join.** A goroutine whose last act is signaling a channel, and a spawner that waits on it, is ordered too:

```go
func worker(xs, done) { xs[0] = 99  done <- 1 }   // signal is the last thing
func main() {
    data := []int{0, 0}
    done := make(chan int)
    go worker(data, done)
    x := <-done          // wait for it
    print(data[0] + x)   // clean — the write already happened
}
```

Both compile without complaint. And the soundness never bends: move the `done <- 1` *before* the write, or receive fewer times than you spawned, and the race comes right back. The barrier only fires when the ordering is provable.

## Soundness, honestly

I hold this to a borrow checker's standard: **never miss a real race** on the surface it covers, even at the cost of occasionally over-reporting. It over-approximates concurrency and ignores which slice *index* you touch — sound, conservative, no false negatives where it claims coverage.

And it's tested like it. Beyond a unit suite spanning write/write, read/write, globals, channel moves, closures captured into goroutines, and every happens-before case, I ran it against five real concurrent machin apps — a health checker, a link checker, a worker pool, a pipe multiplexer, a websocket client. **Zero false positives.** They all share by communicating over channels — the safe pattern — so there was correctly nothing to flag. Then I injected a real shared-slice race into one, and it was caught. Clean means safe, not blind.

The corpus even found a bug in *my* analysis: goroutines spawned in a loop are N concurrent instances from one line of source, and my first cut counted that line once. That's a *missed* race — the worst kind — and it's exactly what validating against real code surfaces. Fixed, regression-tested, and the honest limits are written down.

## This is what machine-first buys you

I'm not claiming machin out-Rusts Rust. Rust has had a decade and a large team on ownership types, and its guarantee covers more than this one does today. The claim is narrower and, I think, more interesting: **on the axis of "safety without ceremony," a language built for a machine to write has a structural advantage a language built for humans can't copy.**

Rust asks the human to annotate what may cross a thread boundary. Machin asks nothing — because the code was written by an agent that had the whole program in view, and the compiler infers the sharing directly from it. Same class of guarantee. No `Send`, no `Sync`, no lifetimes. The machine writes the concurrent code; the compiler proves it race-free; you read the counterexample if it can't.

That's the whole bet behind machin — that designing a language for machines instead of humans doesn't mean *less* rigor, it means you can afford *more*, because the tax that made rigor expensive was always a human tax.

The compiler that inferred all of this is itself written in machin, and compiles itself. But that's another post.

---

*machin is open source at [github.com/javimosch/machin](https://github.com/javimosch/machin). It's built by [Javier Arancibia](https://intrane.fr) — the same engineering that goes into [intrane.fr](https://intrane.fr).*
