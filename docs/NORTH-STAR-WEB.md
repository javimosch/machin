# Web / frontend: a north star for the domain

machin is a **backend-first** language — and it already has a real backend-web
story: [`framework/machweb.src`](../framework) is a tiny HTTP framework written
in MFL that compiles to a single self-contained native binary. This document is
about the *other* half of the web: the **frontend**, machin running **in the
browser** via WebAssembly — the way Rust does with Yew / Leptos / Dioxus.

> Like the [game-dev north star](./NORTH-STAR-GAMEDEV.md), this is a **branch** of
> the machine-first core, not a promise. It records the direction so the next demo
> knows which gap is worth driving. Several tiers below are ideas, not commitments.

## The bet, and why it's reachable

Rust reaches the browser through **LLVM → wasm** plus DOM bindings. machin's
analog is **MFL → C → wasm**, and the C step already exists. The proof is
[machin-demo-wasm](https://github.com/javimosch/machin-demo-wasm): an interactive
counter whose entire view (markup, arithmetic, a recursive `fib`) is MFL compiled
to a `.wasm` and rendered live in Chrome, with JS doing nothing but forwarding
events and painting the string machin emits.

Two pieces of luck make it work with **no new heavy toolchain**:

- **[zig](https://ziglang.org) is a single-binary C→wasm cross compiler** (it
  ships clang + a wasi-libc). `zig cc -target wasm32-wasi -mexec-model=reactor`
  turns machin's emitted C into a wasm module. No emscripten, no wasi-sdk.
- **The bridge is just machin's existing FFI, both ways.** An exported MFL
  function is a wasm export JS calls into; an `extern "env" { fn set_html(string) }`
  block becomes a wasm **import** the JS host supplies (the DOM/effect bridge).
  Strings cross as a pointer into wasm memory the host decodes; ints are i64 ⇒
  `BigInt` on the JS side.

So the frontend is reachable **today** with a shell script of workarounds. The
north star is to make it first-class.

## Where it stands

| capability | repo | what it proved |
|---|---|---|
| **`--target wasm` (first-class, v0.50.0)** | this repo | `machin build --target wasm` → a `wasm32-wasi` reactor module via `zig cc`; `export func` + FFI-as-import bridge built into codegen; lean pay-as-you-go runtime |
| **MFL in the browser** | [machin-demo-wasm](https://github.com/javimosch/machin-demo-wasm) | MFL → C → wasm; FFI-as-import DOM bridge; string marshaling; verified on-screen in Chrome |
| **isomorphic SSR** | [machin-demo-ssr](https://github.com/javimosch/machin-demo-ssr) | one `view.src` compiled into both a native machweb server (HTML per request) and the wasm client — server HTML and client re-render byte-identical |
| backend web framework | [`framework/machweb.src`](../framework) | `serve(port, handler)` → self-contained native server |

## The feature roadmap (gaps, in rough dependency order)

Each is a candidate to be *driven by a demo*, not built speculatively. The first
five shipped in **v0.50.0** (`--target wasm`); the rest remain.

1. ~~**A `--target wasm` codegen mode.**~~ **Done (v0.50.0).** The POSIX
   networking (`listen`/`accept`/`dial`/`read`/`write`/`close`) and tty
   (`raw_mode`/`read_key`) runtimes are split out of the always-on core and made
   **pay-as-you-go** — the same `usesX` gating used for TLS/WSS/regex/math/SQLite.
   Native is unchanged; a wasm build references no `socket()`/`termios`, so it
   links clean. The keystone.
2. ~~**Emit wasm-import attributes.**~~ **Done (v0.50.0).** Under the wasm target a
   headerless `extern "<mod>"` emits `__attribute__((import_module, import_name))`,
   so host functions are proper wasm imports automatically.
3. ~~**An `export` annotation.**~~ **Done (v0.50.0).** `export func` marks a wasm
   export (under its clean source name via `export_name`) and a reachability root,
   so a module needs no dummy `main`.
4. ~~**A `machin build --target wasm` driver.**~~ **Done (v0.50.0).** The `zig cc`
   invocation is folded into the compiler (`BuildWasm`), so `app.wasm` falls out of
   one command (`ZIG=` overrides the toolchain).
5. **Package-level mutable state.** MFL has no top-level `var`; today the JS host
   owns app state. Either add globals, or settle on a **handle-based** pattern
   (`alloc` a state struct, return the pointer, pass it back into each call). The
   next gap to drive.
6. **String/array marshaling helpers.** A small JS runtime shipped with the
   target (decode/encode strings, pass slices) so apps don't re-roll `readCString`.

## The vision, in tiers (near → far)

**Tier 1 — MFL modules in the page (here).** Pure/compute logic and view-string
builders compiled to wasm, called from hand-written JS. Already works; the
roadmap above just removes the friction.

**Tier 2 — a thin DOM runtime.** A small shipped JS + MFL layer: mount a root,
call an MFL `render(state) -> html string`, set `innerHTML`. Event handlers route
back into exported MFL reducers. Enough to write a real single-page widget with
**all logic in machin** and no framework.

**Tier 3 — a reactive framework (the Leptos/Yew analog).** Components, signals,
and a **virtual-DOM diff** (or fine-grained reactive nodes) living in MFL;
JS becomes a tiny host that applies a patch list machin computes. This is where
"machin on the frontend" becomes ergonomic rather than a proof. Wants the
vector/array marshaling layer and probably FFI **callbacks** (host → MFL, shared
with the game-dev roadmap's Phase-4 callbacks).

**Tier 4 — full-stack MFL (started).** One language across the wire: a component
written once in MFL, run on the [`machweb`](../framework) server for first paint
and in wasm for interactivity. [machin-demo-ssr](https://github.com/javimosch/machin-demo-ssr)
is the first step — a shared `view.src` compiled into both the native server and
the wasm client, producing byte-identical HTML. Still ahead: a single binary that
**serves its own wasm + assets** (wants a machweb bytes-body response — a `.wasm`
has NUL bytes a C-string body truncates), shared MFL types/(de)serialization
across the wire, and server-rendered-then-**hydrated** pages (reuse the SSR DOM,
attach the wasm client). The richer reaches (typed RPC, streaming) stay
aspirational.

## Method

Same loop as the rest of machin: build a real thing, hit the wall, fill it in the
language, release, record. The frontend's first wall — the unconditionally-POSIX
core runtime — was hit by [machin-demo-wasm](https://github.com/javimosch/machin-demo-wasm)
and filled in **v0.50.0** (`--target wasm`); the next wall is package-level state
(gap #5), to be named by the next demo. Be honest about
**composition vs. new feature**: that the FFI *already* doubles as the wasm
import/export bridge is composition, and itself the headline result — machin
reaches the browser with no new language primitive, only a leaner emit.

To contribute a frontend demo: build something real, put it in its own public
repo with a `build.sh`, and add it to
[awesome-machin](https://github.com/javimosch/awesome-machin).
