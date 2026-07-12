# Inference: a north star for the domain

machin is a **backend-first, machine-first** language — its core target is
servers, tools, and protocols. But the dogfood loop (build real things; let real
use surface gaps) keeps pulling it toward a new frontier: running **quantized
neural-network inference** as a single static native binary, no Python, no
`libtorch`, no cgo. Dogfooding a small LLM engine (**machin-colibri**, the
worker-pool-over-mmapped-weights engine that surfaced issues [#434](https://github.com/javimosch/machin/issues/434)
and drove the kernel builtins below) has turned "AI inference" into one of
machin's freshly detected domains. This document is the north star for it.

> The overall machin north star is unchanged (backend, machine-first). Inference
> is a **branch** of it — a domain machin is being extended toward, one verified
> builtin at a time — exactly like the [game-dev](./NORTH-STAR-GAMEDEV.md),
> [web](./NORTH-STAR-WEB.md), and [backend](./NORTH-STAR-BACKEND.md) branches.
> Several tiers below are aspirations, not commitments.

## Why machin, for this domain

Inference at the edge is a *systems* problem before it is a math problem: mmap a
multi-gigabyte checkpoint without copying it, keep peak RSS flat while streaming
tokens, saturate the SIMD units on the int8 hot loop, and start cold in
milliseconds. That is precisely the shape machin already optimizes for — one
static musl binary, a per-goroutine arena that bounds memory, and a C backend
whose autovectorizer machin can feed directly. The weights are the model; the
*engine* around them is plumbing, and plumbing is what machin is for.

The bet is narrow and honest: machin will not train models and will not chase the
GPU datacenter path. It aims to be an excellent **host for already-quantized
weights on CPU** — the `llama.cpp`-shaped niche — where a tiny, dependency-free,
fast-cold-start binary is worth more than peak FLOPS.

## Where it stands today (all shipping, all verified)

The int8 quantized-matmul kernel is the `sha256` of this domain — the one hot
primitive everything else composes around — and it now exists as a native
builtin, built up in three dogfood-driven steps:

| capability | builtin(s) | landed | what it proved |
|---|---|---|---|
| zero-copy weight load | `mmap_file` (+ map auto-grow) | [#436](https://github.com/javimosch/machin/pull/436) (2026-07-11) | mmap a checkpoint read-only → `(ptr, len)`; pages fault in lazily; ~700× faster large-file/tokenizer load vs `read_file_bytes` + copy |
| raw int8 access | `peek_i8` / `peek_u8` | [#435](https://github.com/javimosch/machin/pull/435) (2026-07-11) | read a signed/unsigned byte at `ptr+offset` out of the mapped buffer, sign- or zero-extended |
| the group kernel | `dot_i8` | [#435](https://github.com/javimosch/machin/pull/435) (2026-07-11) | signed-byte dot product of two raw buffers with a **32-bit accumulator** — the width the C autovectorizer turns into `vpmaddwd`/`vpdpbusd`, where an int64 reduction stays half-speed. Exact while `|sum| < 2^31` (always, for i8·i8 up to n ≈ 133k) |
| the whole inner product | `dot_q8` | [#439](https://github.com/javimosch/machin/pull/439) (2026-07-12) | grouped, **dual-scaled** int8 dot: the entire Q8_0 quantized-matmul inner product in one vectorized call, one int32 reduction per group with the two per-group fp32 scales applied group-at-a-time |

Supporting primitives already in the language carry the rest: `alloc`/`free`
(raw C buffers, pointers as `int`), `poke_f32`/`poke_i32`/`poke_u8` (build the
quantized buffers), `peek_f32`/`peek_i32` (read the scales), `f64_bits`/
`f64_from_bits` (byte-level float (de)serialization), goroutines + channels (a
worker pool over rows), and `arena { … }` (bound the per-token allocation).

The how-to substrate for the surrounding engine is the backend/web skills; the
kernel builtins are catalogued in `machin guide` (the `memory` group).

### The Q8_0 layout, concretely

`dot_q8(xq, xs, wq, ws, n, gs)` computes the dual-scaled grouped dot product used
by Q8_0 weights (the format `llama.cpp` calls `Q8_0`):

- `xq`, `wq` — two `int8` buffers of `n` bytes each (the quantized activation and
  weight rows). Read with `peek_i8`; written with `poke_u8` today (see the gap
  below).
- `xs`, `ws` — two `fp32` buffers of `n/gs` floats each (the per-group scales).
  Read/written with `peek_f32`/`poke_f32`.
- `gs` — the group size (`n` must be a multiple of `gs`); Q8_0 uses 32.

It returns, in one call (autovectorized, one int32 reduction per group):

```
sum over g in [0, n/gs) of:
    ( sum over k in [0, gs) of xq[g*gs+k] * wq[g*gs+k] )   // int32 group dot
  * xs[g] * ws[g]                                          // fp32 group scales
```

That is a full GEMV inner product. Wrapped in an MFL `for` over the weight rows
(one `dot_q8` per output element), it is a quantized matrix–vector product — the
per-token compute of a transformer's linear layers. `dot_q8` exists precisely
because doing this as an MFL loop of `dot_i8` + two `peek_f32` per group made the
per-group *call overhead* the bottleneck; folding the group loop into one builtin
removed it.

## The vision, in tiers (near → far)

**Tier 1 — the quantized GEMV kernel (here).** mmap the weights, `dot_q8` the
rows, stream the output. Everything needed for a single quantized linear layer
ships today. This is the load-bearing tier and it is real.

**Tier 2 — a whole transformer block (composition + a few kernels).** A block is
GEMV (have it) plus a small set of elementwise/reduction ops: **RMSNorm**,
**SiLU/GELU**, a **softmax** over the attention scores, **RoPE** position
rotation, and an **argmax**/sampling step over the logits. Each is a tight loop
over an fp32 buffer — expressible in MFL today over `peek_f32`/`poke_f32`, and a
candidate to promote to a builtin *only where measurement says the call/loop
overhead caps throughput* (the exact reason `dot_q8` was carved out). This tier is
mostly plumbing over existing primitives.

**Tier 3 — a running small model (aspirational).** End-to-end token generation
for a small quantized model (a TinyLlama / Qwen-0.5B-class checkpoint), decode
loop and KV cache included, at a usable tokens/sec on commodity CPU. The engine
is `machin-colibri`; the language gaps it hits become this roadmap.

**Tier 4 — the edge-inference niche (stretch, not a plan).** A dependency-free
inference binary small and fast enough to embed anywhere machin already goes — a
webhook that classifies, a CLI that summarizes, a game NPC that talks — reusing
the same static-musl, FROM-scratch-image story as the rest of machin. Listed as a
direction, not a commitment.

## The feature roadmap (gaps, in rough dependency order)

Each is a candidate to be *driven by the engine*, not built speculatively — the
same method as every other machin domain.

1. ~~**int8 group kernel**~~ — **done:** `dot_i8` ([#435](https://github.com/javimosch/machin/pull/435)),
   then `dot_q8` ([#439](https://github.com/javimosch/machin/pull/439)) for the
   dual-scaled grouped form.
2. ~~**Zero-copy weight load**~~ — **done:** `mmap_file`
   ([#436](https://github.com/javimosch/machin/pull/436)); pages fault in lazily,
   so a multi-GB checkpoint costs no upfront copy and no resident bloat.
3. **`poke_i8` — close the signed-byte asymmetry.** `peek_i8` reads a signed
   byte, but there is **no `poke_i8`**: an MFL quantizer that produces the `int8`
   weight/activation buffers must write them through `poke_u8` and rely on
   two's-complement wrapping (`poke_u8(p, o, v & 0xff)`), which is correct but
   asymmetric and easy to get subtly wrong. Adding `poke_i8` completes the pair
   the way `peek_u8`/`poke_u8`, `peek_i32`/`poke_i32`, and `peek_f32`/`poke_f32`
   already are, and makes the quantize→store side as clean as the load→dot side.
   The smallest, most obviously-missing next step.
4. **Elementwise / reduction kernels (measure first).** RMSNorm, SiLU/GELU,
   softmax, RoPE, argmax. Start as MFL over `peek_f32`/`poke_f32`; promote the
   ones that measurably cap throughput to builtins, exactly as `dot_q8` was.
5. **More quantization formats.** Q4_0 / Q4_K-style 4-bit weights (a nibble-unpack
   before the int8 dot) widen which off-the-shelf checkpoints load.
6. **Parallel GEMV over the row range.** A goroutine worker pool already exists;
   the open question ([#434](https://github.com/javimosch/machin/issues/434)) is
   race-inference for globals set once at load and read by every worker — a
   race-checker refinement, not a kernel.

## Method

Same loop as the rest of machin: build a real thing (the `machin-colibri`
engine), hit the wall, fill it in the language, release, and record it. Be honest
about **composition vs. new feature** — the Tier-2 elementwise ops are *mostly
composition* over `peek_f32`/`poke_f32`, and a kernel is carved out only when a
profile says the loop overhead, not the arithmetic, is the ceiling. `dot_q8` is
the model: it earned its place by replacing a measurably slower MFL loop, not by
speculation. The next op the engine genuinely *can't* express fast enough names
the next builtin.

To contribute: build something real on the inference primitives, put it in its own
public repo with a `build.sh`, and add it to
[awesome-machin](https://github.com/javimosch/awesome-machin).
