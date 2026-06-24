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
| **MFL in the browser** | [machin-demo-wasm](https://github.com/javimosch/machin-demo-wasm) | MFL → C → `zig cc` wasm; FFI-as-import DOM bridge; string marshaling; verified on-screen in Chrome |
| backend web framework | [`framework/machweb.src`](../framework) | `serve(port, handler)` → self-contained native server |

## The feature roadmap (gaps, in rough dependency order)

Each is a candidate to be *driven by a demo*, not built speculatively. The demo
above already names the first four — its `build.sh` papers over each:

1. **A `--target wasm` codegen mode.** The core C runtime unconditionally
   references POSIX **networking/tty** symbols (`socket`, `SO_REUSEADDR`,
   `termios`, pthreads). A frontend app calls none of them. Make that runtime
   **pay-as-you-go** — machin already gates `usesWSS` / `usesRegex` / `usesMath` /
   `usesNoise` / `usesSQLite` this way, so the same pattern applied to
   networking/tty/goroutines yields a lean, freestanding-friendly emit. This is
   the keystone; everything else is small.
2. **Emit wasm-import attributes.** Under the wasm target, an FFI `extern` should
   emit `__attribute__((import_module("env"), import_name("...")))` instead of a
   bare prototype, so host functions become proper wasm imports automatically.
3. **An `export` annotation.** Mark which MFL functions are wasm exports (and keep
   them through tree-shaking) without needing a dummy `main` to reference them.
4. **Package-level mutable state.** MFL has no top-level `var`; today the JS host
   owns app state. Either add globals, or settle on a **handle-based** pattern
   (`alloc` a state struct, return the pointer, pass it back into each call).
5. **A `machin build --target wasm` driver.** Fold the `zig cc` invocation into
   the compiler (like `BuildBinary` does for native), auto-detecting/honoring a
   `zig`/wasi toolchain, so `app.wasm` falls out of one command.
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

**Tier 4 — full-stack MFL (aspirational).** One language across the wire: the
[`machweb`](../framework) server and the wasm client share MFL types and
(de)serialization, so a request/response or a server-rendered-then-hydrated page
is written once. A stretch goal, listed as a direction, not a plan.

## Method

Same loop as the rest of machin: build a real thing, hit the wall, fill it in the
language, release, record. The frontend's first wall is already standing and
named — the unconditionally-POSIX core runtime (gap #1). Be honest about
**composition vs. new feature**: that the FFI *already* doubles as the wasm
import/export bridge is composition, and itself the headline result — machin
reaches the browser with no new language primitive, only a leaner emit.

To contribute a frontend demo: build something real, put it in its own public
repo with a `build.sh`, and add it to
[awesome-machin](https://github.com/javimosch/awesome-machin).
