---
name: machin-web
description: Build web apps in machin (MFL) — a native HTTP server, a JSON API, server-side rendering, and a reactive WebAssembly UI, all in one language with no Node/bundler. Use when writing or debugging a machin web app: a backend service, an SSR page, a wasm SPA, an isomorphic full-stack app, or a CRUD back-office. Covers the machweb/reactive/flags frameworks, the wasm bridge, host↔wasm marshaling, the generic JS host, build-and-verify, and the hard-won gotchas. Distilled from the web-domain dogfood (machin v0.50–v0.58).
---

# Building web apps in machin

machin compiles MFL to a **native binary** (the server) *and* to **WebAssembly**
(the browser client). One language does both ends of the wire — server, API, SSR
HTML, and a fine-grained reactive UI — with **no Node, no bundler, no
`node_modules`**. The JS host is a fixed ~30 lines; everything else is MFL.

> Run `machin guide` first — it's the version-exact language catalog (builtins,
> idioms, gotchas), including a `reactive-runtime` note. This skill is the *web how-to*.

## The fastest path: start from the boilerplate

[`boilerplate-cli-ui-machin-isomorphic`](https://github.com/javimosch/boilerplate-cli-ui-machin-isomorphic)
is a working single binary that is a **CLI + HTTP server + JSON API + reactive wasm
UI** (SSR + hydration, shared model). Clone it and edit — it already wires up
everything below. To build something new, copy its structure:

```
src/machweb.src  src/flags.src  src/reactive.src   # vendored frameworks (from machin/framework)
src/models.src        # shared schema/render, compiled into BOTH server and client
src/client.src        # the wasm UI (reactive)
src/server.src        # machweb routes + CLI main; serves its own wasm + the API
web/host.js           # the generic JS host, embedded into the binary at build time
build.sh
```

`build.sh` does two compiles: the client to wasm, then the server (which embeds
`web/host.js` and serves `app.wasm`):

```sh
machin encode src/reactive.src src/models.src src/client.src > client.mfl
machin build  client.mfl --target wasm -o app.wasm                       # the SPA
python3 -c "import json;print('func host_js() (s) { s = '+json.dumps(open('web/host.js').read())+' }')" > src/host_gen.src
machin encode src/machweb.src src/flags.src src/models.src src/server.src src/host_gen.src > server.mfl
machin build  server.mfl -o app                                          # the binary
```

Needs `machin` (v0.57.0+ for forms) and [`zig`](https://ziglang.org) (the C→wasm
compiler — a single binary, no emscripten). The frameworks live in
[`machin/framework`](../../framework); vendor copies into your repo.

## The server — `framework/machweb.src`

A handler is `func(Request) Response`. `serve(port, handler)` runs it (one
goroutine per connection). `req.method` / `req.path` (path carries the query
string) / `req.body` / `header(req, name)` / `cookie(req, name)`.

Response builders: `ok_text` · `ok_html` · `ok_json` · `ok_bytes(ctype, b)` ·
**`ok_wasm(b)`** · `created` · `bad_request` · `not_found`.

```go
func handle(req) (res) {
    if req.path == "/app.wasm" { return ok_wasm(read_file_bytes("app.wasm")) }  // serve your own SPA
    if has_prefix(req.path, "/api/users") { return ok_json(users_json()) }
    return ok_html(page())                                                       // SSR
}
func main() { serve(48080, func(req) { return handle(req) }) }
```

- **Serve your own wasm:** `ok_wasm(read_file_bytes("app.wasm"))` — `read_file_bytes`
  is NUL-safe (a `.wasm` has NUL bytes a string body would truncate).
- **A database:** machin has SQLite builtins — `sqlite_open(path)` / `sqlite_exec(db,
  sql)` / `sqlite_exec(db, sql, []string params)` (`?`-bind, injection-safe) /
  `sqlite_query(db, sql[, params]) -> string` (a **JSON-array-of-rows string**) /
  `sqlite_close(db)`. Perfect for a users table behind a JSON API. **Decode rows with
  `parse`, not `json_get`:**
  ```go
  type User struct { id int  name string  email string }
  rows  := sqlite_query(db, "SELECT id, name, email FROM users ORDER BY id")
  users := parse(rows, []User{})            // a typed []User to range over — values decoded
  ```
  A **slice witness** (`[]User{}`) is the row-iteration idiom. Reach for `json_get(rows,
  "[0].name")` only to pull ONE field — and note **it returns the raw JSON token, so a
  string comes back quoted** (`"Ada"`); strip the quotes (`substr(s, 1, len(s)-1)`) or
  just use `parse`. Always parameterize (`?`, `[]string{...}`); never concat user input.
- **A CLI:** compose `framework/flags.src` — `new_flags` / `flag_int` / `flag_str` /
  `flag_bool` / `parse_flags(fs, args())` / `flag_int_val` / `flag_on(fs,"help")`
  (auto `--help`).
- **Cookies & login sessions:** `cookie(req, name)` reads a request cookie;
  `set_cookie(res, name, value)` / `clear_cookie(res, name)` return a `Response` with
  a `Set-Cookie` (safe defaults: `Path=/; HttpOnly; SameSite=Lax`). For login, use a
  **signed** session the client can't forge: `set_session(res, secret, "sid", userID)`
  stores `userID` + an HMAC tag, and `get_session(req, secret, "sid") -> (value, ok)`
  returns `ok == 1` only if it verifies. `Response` is a value, so chain the helper:
  ```go
  func login(req) (res) {
      // ...check credentials...
      res = set_session(ok_html(dashboard()), secret, "sid", "user:42")
  }
  func page(req) (res) {
      uid, ok := get_session(req, secret, "sid")
      if ok == 0 { return set_cookie(not_found(), "x", "")  /* or redirect to /login */ }
      res = ok_html(home(uid))
  }
  ```
  Keep `secret` server-side (e.g. `env("SESSION_SECRET")`). The value is signed, not
  encrypted — store an id/handle, not secrets.

## The client — `framework/reactive.src` (signals + a patch list)

The Solid/Leptos model in MFL: state in **signals**, fine-grained updates. Only the
reactions that read a changed signal recompute, and only changed text/keys touch
the DOM (no `innerHTML` churn, no vdom diff).

| call | what it does |
|---|---|
| `signal(v) -> id` | a state cell; `get(id)` reads (auto-tracks a dependency), `set(id, v)` writes (notifies dependents if changed) |
| `computed(func(){ return … }) -> id` | a memoized derived signal; read with `get` like any signal |
| `slot(name, compute) -> markup` | markup for a reactive text node (`<span data-s=name>`) + queues its binding |
| `list(name, keys, item) -> markup` | markup for a keyed list + queues its reconciler. `keys()` returns the ordered keys as a **CSV string**; `item(key)` returns one item's HTML |
| `mount(root, html)` | set the root's innerHTML once, then activate the queued slots/lists (client-side render) |
| `hydrate(html)` | activate them against an **already-SSR'd DOM** (no re-render) — for isomorphic pages |

**Multi-page (`framework/router.src`).** For an admin app with several pages, compose
the router too. The active route is a signal (an int index): `router_init(0)`,
`route(path)` registers pages in order, `link(path, label)` renders a nav anchor,
`current_route()` reads the active index, and `outlet(id, render)` re-renders the
active page (a reaction over a `dom_html` host import) and syncs the address bar.
`page()` switches on `current_route()` and must be a **pure render** (read signals,
never `set` — that loops). The host adds `dom_html`/`nav_url`, forwards `[data-nav]`
clicks to `nav(path)` (path in via `ptr_str`) + `popstate`, and a catch-all server
serves the shell for any path (deep-links). See [machin-web-demo-router](https://github.com/javimosch/machin-web-demo-router).

A whole component is one expression:

```go
export func start() {
    n = signal(0)
    total = computed(func() { get(ver)  return sum_of(items) })
    mount("app",
        "<h1>Items</h1>" +
        slot("total", func() { return str(get(total)) }) +
        list("items", func() { get(ver)  return csv(ids) }, func(id) { return row(id) }))
}
```

The host supplies five DOM ops as imports: `dom_mount` · `dom_patch` ·
`list_insert` · `list_remove` · `list_order` (see the host below).

**Keyed lists only re-render an item on INSERT**, not when a kept item's content
changes. To make a row update when its data changes, **encode the mutable state in
the key** (`key = id*100 + done`); a change makes a new key → one remove+insert.

## The wasm bridge & marshaling

- **JS → machin:** `export func name(...)` becomes a wasm export the host calls
  (`instance.exports.name`). A wasm module needs no `main`.
- **machin → JS:** a headerless `extern "env" { fn dom_patch(string, string) }`
  becomes a wasm import the host supplies.
- **ints** cross both ways as **`BigInt`** (machin ints are i64).
- **strings out:** machin returns a pointer into `memory`; the host decodes
  NUL-terminated UTF-8 (`let e=p; while(b[e])e++; TextDecoder().decode(b.subarray(p,e))`).
- **strings in (forms):** the host writes UTF-8 into a buffer the module `alloc`'d,
  then machin reads it with **`ptr_str(ptr)`** (v0.57.0):
  ```go
  export func input_buf(n) (p) { p = alloc(n + 1) }       // host writes n bytes + NUL here
  export func submit(p) { let t = ptr_str(p)  free(p)  /* …use t… */ }
  ```

## The generic JS host (reusable as-is)

```js
let mem; const dec = new TextDecoder(), enc = new TextEncoder();
const cstr = p => { const b = new Uint8Array(mem.buffer); let e=p; while(b[e])e++; return dec.decode(b.subarray(p,e)); };
const env = {
  dom_mount:  (r,h) => { document.getElementById(cstr(r)).innerHTML = cstr(h); },
  dom_patch:  (s,v) => { const el = document.querySelector('[data-s="'+cstr(s)+'"]'); if (el) el.textContent = cstr(v); },
  list_insert:(c,k,h)=> { const li=document.createElement('li'); li.dataset.k=cstr(k); li.innerHTML=cstr(h); document.getElementById(cstr(c)).appendChild(li); },
  list_remove:(c,k) => { const el=document.querySelector('#'+cstr(c)+' > [data-k="'+cstr(k)+'"]'); if (el) el.remove(); },
  list_order: (c,csv)=> { const ct=document.getElementById(cstr(c)); for (const k of cstr(csv).split(',').filter(Boolean)) { const el=ct.querySelector('[data-k="'+k+'"]'); if (el) ct.appendChild(el); } },
  // …your app's effect imports, e.g. api_vote, focus_input…
};
const wasi = { fd_write:()=>0, fd_seek:()=>0, fd_close:()=>0, fd_fdstat_get:()=>0 };  // no-op shim (see gotchas)
const { instance } = await WebAssembly.instantiateStreaming(fetch('/app.wasm'), { env, wasi_snapshot_preview1: wasi });
mem = instance.exports.memory; instance.exports._initialize?.();
instance.exports.start();
// write a string IN: const b=enc.encode(text); const p=Number(instance.exports.input_buf(BigInt(b.length))); new Uint8Array(mem.buffer).set(b,p); instance.exports.submit(BigInt(p));
```

## Build & verify (this environment)

- `zig` is on PATH (`/snap/bin/zig`); `machin build --target wasm` uses it.
- Serve over **http** (`python3 -m http.server`) — wasm won't load from `file://`.
- **Screenshot** to verify rendering: `google-chrome --headless=new --screenshot=/tmp/x.png --window-size=W,H http://localhost:PORT/` then read the PNG. To exercise interaction, inject a small autopilot `<script>` (set an input's value + click) since there's no click injector.
- Verify reactivity headlessly in **node**: instantiate `app.wasm` with stub imports that record `dom_patch`/`list_*` calls, drive the exports, and assert the patch list is minimal.

## Gotchas (hard-won)

- **No-op WASI stub:** the reactive runtime's indirect closure calls keep wasi-libc's
  float-`snprintf` path, so the wasm imports `wasi_snapshot_preview1.{fd_write,fd_seek,fd_close,fd_fdstat_get}`. They're never *called* — provide the ~4 no-op stubs above.
- **A function named like a builtin is a COMPILE ERROR** (`flush`/`keys`/`contains`/`len`/`str`/…). Rename it. (An `extern` may shadow — that's for FFI.)
- **Lambdas have no named returns:** `func() (s) { s = x }` doesn't parse — use `func() { return x }`.
- **Function scope, not block scope:** a variable used as two types across `if` branches conflicts; give each branch its own name.
- **Package globals** (`var x = 0` at top level) hold a component's state across export calls (persist in the wasm instance); `=` assigns the global, `:=` shadows with a local.
- **Closures over a loop variable** see its final value (captured by reference) — build per-item closures via a helper that takes the index as a *parameter* (fresh per call), not inside the loop.
- **Two `extern "env"` blocks compose fine** (the runtime's DOM ops + your app's effect imports).

## Recipe: a CRUD back-office (e.g. manage a users DB)

1. **Schema** (`models.src`, shared): a `user_row(id, name, email) -> html` used by
   both SSR and the client list item.
2. **Server** (`server.src`): `sqlite_open("users.db")`, create the table; routes —
   `GET /` SSR-renders the user list (matching `data-s` names so the client
   hydrates), `GET /app.wasm` serves the client, `GET /api/users` returns rows as
   JSON (`ok_json(json(parse(sqlite_query(...), []User{})))` or just the raw query
   string), `POST /api/users` inserts (parse the body), `DELETE`/`POST /api/users/del`
   removes. Use parameterized `sqlite_exec(db, sql, params)` — never string-concat SQL.
   - **The POST body shape depends on the sender.** A `fetch` with a JSON body →
     `parse(req.body, User{})` or `json_get`. A browser `<form>` (non-JS fallback, or
     `Content-Type: application/x-www-form-urlencoded`) → the body is `name=Ada&email=a%40b`;
     decode each field with the **`url_decode` builtin** (don't hand-roll it — a function
     named `url_decode` is a compile error, it shadows the builtin). A tiny helper:
     ```go
     func form_field(body, key) (v) {           // body: "a=1&b=2"; key: "a"
         for _, kv := range split(body, "&") {
             eq := index(kv, "=")
             if eq > 0 && substr(kv, 0, eq) == key { v = url_decode(substr(kv, eq+1, len(kv))) }
         }
     }
     ```
3. **Client** (`client.src`): signals hold the rows; `each` renders the table
   (re-key on any mutable field); a **form** (`<input>` + `ptr_str`) adds a user and
   `POST`s; row buttons toggle/delete and call the API. A `computed` shows the count.
4. **Host** (`web/host.js`): the generic host + your `api_*` effect imports (each
   does a `fetch`).
5. **Build & screenshot** to verify.

The state lives in machin; the server is the source of truth (SQLite); the client is
reactive over the API. One binary, one language.

> Worked implementation: [machin-web-demo-users](https://github.com/javimosch/machin-web-demo-users) — the exact app above, end to end. (For the *isomorphic* shape instead, see the boilerplate.)

## Pointers

- Runnable in-tree: [`examples/complex/sqlite_crud.mfl`](../../examples/complex/sqlite_crud.mfl) — the SQLite data layer end to end (open → parameterized insert → `parse([]User{})` → update/delete → JSON), no server, runs under `machin run`.
- Frameworks: [`framework/machweb.src`](../../framework) · `reactive.src` · `router.src` · `flags.src`.
- Boilerplate: [boilerplate-cli-ui-machin-isomorphic](https://github.com/javimosch/boilerplate-cli-ui-machin-isomorphic).
- Focused demos: [machin-web-demo-wasm](https://github.com/javimosch/machin-web-demo-wasm) (the bridge) · [machin-web-demo-ssr](https://github.com/javimosch/machin-web-demo-ssr) (isomorphic SSR) · [machin-web-demo-reactive](https://github.com/javimosch/machin-web-demo-reactive) (signals/computed/lists/templating) · [machin-web-demo-todo](https://github.com/javimosch/machin-web-demo-todo) (forms / text input).
- Direction & the dogfood record: [`docs/NORTH-STAR-WEB.md`](../../docs/NORTH-STAR-WEB.md).
- The language itself: `machin guide`.
