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
[machin-web-demo-wasm](https://github.com/javimosch/machin-web-demo-wasm): an interactive
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

> **Building a web app?** The how-to substrate is
> [`skills/machin-web/SKILL.md`](../skills/machin-web/SKILL.md) — the agent-facing
> guide to the server (machweb), the reactive client, the wasm bridge + marshaling,
> the generic JS host, build-and-verify, the gotchas, and a CRUD back-office recipe.
> Start from the [boilerplate](https://github.com/javimosch/boilerplate-cli-ui-machin-isomorphic).

## Where it stands

| capability | repo | what it proved |
|---|---|---|
| **`--target wasm` (first-class, v0.50.0)** | this repo | `machin build --target wasm` → a `wasm32-wasi` reactor module via `zig cc`; `export func` + FFI-as-import bridge built into codegen; lean pay-as-you-go runtime |
| **MFL in the browser** | [machin-web-demo-wasm](https://github.com/javimosch/machin-web-demo-wasm) | MFL → C → wasm; FFI-as-import DOM bridge; string marshaling; verified on-screen in Chrome |
| **isomorphic SSR** | [machin-web-demo-ssr](https://github.com/javimosch/machin-web-demo-ssr) | one `view.src` compiled into both a native machweb server (HTML per request) and the wasm client — server HTML and client re-render byte-identical |
| **fine-grained reactivity** | [machin-web-demo-reactive](https://github.com/javimosch/machin-web-demo-reactive) · [`framework/reactive.src`](../framework) | signals + a patch list (Solid/Leptos model) in MFL: auto-tracked deps, only changed bindings recompute, only changed text patches — drove `[]func` (v0.53.0), computed + keyed lists (v0.54.0), templating (v0.55.0) |
| **reactive forms (text input)** | [machin-web-demo-todo](https://github.com/javimosch/machin-web-demo-todo) | a todo app — typed text flows into wasm via `ptr_str` (v0.57.0); signals + computed + keyed list + templating |
| **CRUD back-office** | [machin-web-demo-users](https://github.com/javimosch/machin-web-demo-users) | one binary = CLI + HTTP server + **SQLite** JSON API + reactive view; `sqlite_query`→JSON + `json_get` + `ptr_str` |
| **multi-page SPA / routing** | [machin-web-demo-router](https://github.com/javimosch/machin-web-demo-router) · [`framework/router.src`](../framework) | client-side router (the active route is a signal); no-reload nav, back/forward, deep-links; built on the `reaction()` primitive (v0.59.0) |
| **isomorphic app boilerplate** | [boilerplate-cli-ui-machin-isomorphic](https://github.com/javimosch/boilerplate-cli-ui-machin-isomorphic) | one binary = CLI + HTTP server + JSON API + reactive wasm UI; SSR + hydrate (v0.56.0); shared `models.src` |
| **local auth + returning imports** | machin-wiki (local) | the first wasm client that *initiates* server calls — `data := http_get(url)` through **returning `extern "env"` imports** (never before exercised in any demo); **PBKDF2** password hashing; signed **session** auth; **SPA catch-all** routing with dynamic `/page/<slug>` params. Drove two compiler changes: the `pbkdf2_sha256` builtin, and gating the wasm target's `sys/wait.h`+`signal.h` (newer zig's wasi-libc `#error`s broke *every* wasm build). Surfaced the `alloc`-is-not-exported requirement for returning imports, and that `json_get` paths can't compose `key[idx].field` — `parse(body, Struct{})` is the idiom |
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
5. ~~**Package-level mutable state.**~~ **Done (v0.52.0).** A top-level
   `var name = expr` is a package global that persists across exported-function
   calls, so a component owns its state in machin (`var count = 0` +
   `export func bump(d){count=count+d}`) instead of the JS host holding it. Any
   type — scalars, strings, make-maps, slices — and visible inside closures.
6. **String marshaling — both directions (done, v0.50.0 + v0.57.0).** Out of wasm:
   machin returns a memory pointer the host decodes (UTF-8 until NUL). Into wasm:
   **`ptr_str(ptr)`** reads a NUL-terminated string the host wrote into an `alloc`'d
   buffer — so a form `<input>`'s text reaches a component
   ([machin-web-demo-todo](https://github.com/javimosch/machin-web-demo-todo)). Ints
   cross both ways as `BigInt`. Still nice-to-have: a shipped JS helper so apps
   don't re-roll the ~6-line decode/encode, and richer types (slices/structs).
7. ~~**Signals + a patch-list runtime.**~~ **Done (v0.53.0).** `framework/reactive.src`
   — signals hold state, bindings are compute closures (a `[]func` registry) tied
   to DOM slots with auto-tracked deps; on `set`, only the bindings that read the
   changed signal recompute, and only those whose text changed emit a patch. Drove
   **`[]func`** (slices of closures). The core of the reactive tier.
8. ~~**Derived/computed signals + list reconciliation.**~~ **Done (v0.54.0).**
   `computed(fn)` (memoized derived signals) and `each(container, keys, item)`
   (keyed list reconciliation — emits only insert/remove/reorder deltas, never
   re-rendering unchanged items). [machin-web-demo-reactive](https://github.com/javimosch/machin-web-demo-reactive)
   is now a reactive list with a computed sum.
9. ~~**A templating helper / component ergonomics.**~~ **Done (v0.55.0).**
   `slot(name, compute)` / `list(name, keys, item)` return markup AND queue their
   reaction; `mount(root, html)` sets the root HTML once then activates them. A
   component now declares its markup + state + reactivity in one place (the JS host
   is generic). Small modular apps are now expressible — the precursor to rewriting
   the [boilerplate](https://github.com/javimosch/boilerplate-cli-ui-machin) as an
   isomorphic machin app.
10. **Builtin-shadowing is a hard error (v0.54.0).** A safety net for app authors:
    a user function named like a builtin is rejected, not silently ignored.

## The vision, in tiers (near → far)

**Tier 1 — MFL modules in the page (here).** Pure/compute logic and view-string
builders compiled to wasm, called from hand-written JS. Already works; the
roadmap above just removes the friction.

**Tier 2 — a thin DOM runtime.** A small shipped JS + MFL layer: mount a root,
call an MFL `render(state) -> html string`, set `innerHTML`. Event handlers route
back into exported MFL reducers. Enough to write a real single-page widget with
**all logic in machin** and no framework.

**Tier 3 — a reactive framework, the Leptos/Yew analog (core reached, v0.53.0).**
Signals + **fine-grained reactive nodes** (not a vdom diff) live in MFL; JS is a
tiny host that applies the patch list machin computes —
[`framework/reactive.src`](../framework) + [machin-web-demo-reactive](https://github.com/javimosch/machin-web-demo-reactive).
A signal change recomputes only its dependent bindings and patches only the slots
whose text changed. Still ahead toward ergonomic: **computed/memoized** signals,
**keyed list reconciliation** (dynamic children), a templating helper so a
component declares its slots without hand-written HTML, and probably FFI
**callbacks** (host → MFL, shared with the game-dev roadmap's Phase-4 callbacks).

**Tier 4 — full-stack MFL (under way).** One language across the wire: a component
written once in MFL, run on the [`machweb`](../framework) server for first paint
and in wasm for interactivity. [machin-web-demo-ssr](https://github.com/javimosch/machin-web-demo-ssr)
now does this in **one binary** — a shared `view.src` compiled into both the
native server and the wasm client (byte-identical HTML), and the server **serves
its own `.wasm`** so the page hydrates with no separate static host. That last
step drove **binary HTTP bodies** in v0.51.0 (`read_file_bytes` + `write_bytes`,
machweb `ok_wasm` — a `.wasm` has NUL bytes a C-string body truncates). Still
ahead: shared MFL types/(de)serialization across the wire (typed RPC), and richer
hydration (reuse the server DOM tree rather than re-rendering). Streaming/RPC at
scale stays aspirational.

## Method

Same loop as the rest of machin: build a real thing, hit the wall, fill it in the
language, release, record. The frontend's first wall — the unconditionally-POSIX
core runtime — was hit by [machin-web-demo-wasm](https://github.com/javimosch/machin-web-demo-wasm)
and filled in **v0.50.0** (`--target wasm`); the next wall is package-level state
(gap #5), to be named by the next demo. Be honest about
**composition vs. new feature**: that the FFI *already* doubles as the wasm
import/export bridge is composition, and itself the headline result — machin
reaches the browser with no new language primitive, only a leaner emit.

To contribute a frontend demo: build something real, put it in its own public
repo with a `build.sh`, and add it to
[awesome-machin](https://github.com/javimosch/awesome-machin).
