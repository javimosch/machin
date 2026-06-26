# Changelog

## v0.76.0

- **File uploads in machweb (multipart/form-data).** Requests are now read **binary-safe**
  — `read_request_bytes`/`parse_request_bytes` keep the body as raw bytes
  (`req.body_bytes`), so an upload with NUL bytes survives intact (the old `read()` path
  truncated at the first NUL). New `parse_multipart(req)` splits a `multipart/form-data`
  body into its `MultipartPart`s (fields + files), with `multipart_file(req, field)` and
  `multipart_field(req, name)` convenience helpers. Responses gain extra headers via
  `with_header(res, name, value)` (e.g. `Content-Disposition` for download filenames).
- **Three enabling builtins**: `bytes_index(haystack, needle, from) -> int` (NUL-safe
  byte search, for binary protocols / multipart boundaries), `write_file_bytes(path,
  bytes) -> int` (binary-safe file write, for storing uploads), and the binary request
  path above. `req.body` (text view) is unchanged for existing apps.
- Dogfooded by **[machin-share](https://github.com/javimosch/machin-share)** — a
  self-hostable file/paste drop in one MFL binary (the upload/download/streaming surface
  that surfaced all of the above).

## v0.75.0

- **A `machin-backend` skill** — the backend domain now has its own how-to (the five
  pooled datastores + the uniform JSON-rows/`parse` idiom, connection pooling, signed
  sessions + SSO, the agent-first CLI contract, daemons, build/verify, gotchas). Embedded
  like the others: `machin guide --skill backend`. The `backend` domain in `machin guide`
  now routes to it.

## v0.74.0

- **Domain how-tos in the binary.** `machin guide` now leads with a **DOMAINS** section
  routing an agent to the right per-domain how-to (web / gamedev / backend), and the
  web + gamedev SKILLs are **embedded** — `machin guide --skill web` / `--skill gamedev`
  print the full guide offline (no repo checkout needed). Closes the gap where an agent
  with only the curl|sh binary reverse-engineered the language from demo repos. The JSON
  guide gains a `domains` array. Web skill now points at the networked DB drivers; both
  skill descriptions refreshed to current coverage.

## v0.73.0

- **MySQL connection pooling** — the MySQL/MariaDB client is now handle-based
  (`MySQLConn`) with an async-channel pool, like the other drivers:
  `mysql_pool_init(n, host, port, user, pass, db)` then `mysql_acquire()` + `myq`/`myx`
  + `mysql_release(c)`. The global `mysql_*` API is unchanged. Verified: 30 goroutines
  over 4 connections, all correct. **All five datastore drivers (Postgres/Redis/Mongo/
  MySQL) are now poolable** (SQLite is embedded).

## v0.72.0

- **MySQL / MariaDB client** (`framework/mysql.src`) — a pure-MFL client over the wire
  protocol, no libmysql. `mysql_connect` (mysql_native_password / SHA-1 challenge auth),
  `mysql_query` (text protocol → a JSON-array-of-rows string, numeric columns unquoted so
  `parse(rows, []T{})` decodes), `mysql_exec` (→ affected rows), `mysql_escape`,
  `mysql_close`. Verified against `mariadb:11`.
- **`sha1_bytes(bytes) -> bytes`** — SHA-1 digest (OpenSSL), for legacy auth like MySQL's
  native password. This unblocked the MySQL handshake.

## v0.71.0

- **MongoDB connection pooling** — the driver is now handle-based (a `MongoConn` =
  fd + read-buffer box) with an async-channel pool, mirroring the Postgres/Redis
  clients. `mongo_pool_init(n, host, port, user, pass)` once, then `mongo_acquire()` /
  the `m*` handle ops (`mins`/`mfind`/`mfindall`/`mfindid`/`mdel`/`mdelid`/`mcount`/
  `mdrop`/`mauth`/`mcmd`) / `mongo_release(c)` per request — so a concurrent server
  (machweb's per-request goroutines) never interleaves on one connection. The global
  `mongo_*` API is unchanged (thin wrappers over one default connection). Verified:
  30 goroutines over 4 pooled connections, every result correct.

## v0.70.0

- **MongoDB: query by `_id` (ObjectId) + filtered finds/deletes.** `bson_oid(acc, key,
  idhex)` encodes an ObjectId from its 24-char hex (the form the decoder produces), so
  you can query by `_id`. New driver helpers: `mongo_find(db, coll, filter)` (an explicit
  BSON filter), `mongo_find_by_id`, `mongo_delete(db, coll, filter)`, `mongo_delete_by_id`.
  `mongo_find_all` is now `mongo_find` with a match-all filter. Closes the ObjectId gap
  the machin-cms dogfood hit (get/delete a document by its id).

## v0.69.0

- **`system(string) -> int`** — run a shell command, returning its exit code (-1 if
  unlaunchable). For process orchestration — e.g. a CLI spawning a detached daemon
  (`system("./app serve >log 2>&1 &")`). Surfaced by the machin-cms dogfood (a
  daemon start/stop mode).

## v0.68.0

- **`parse_float(string) -> float`** — parse a floating-point number (strtod; 0.0 on
  non-numeric), the float counterpart to `parse_int`. Lets a tool turn a textual number
  (a SQLite REAL read as JSON, a CLI arg) into an MFL float — e.g. carry REAL columns
  into Mongo doubles in `machin-db-migrate`.

## v0.67.0

- **MongoDB client v2** (`framework/mongo.src` + `bson.src`): **SCRAM-SHA-256 auth**
  (`mongo_auth(authdb, user, password)` — the SASL conversation via `saslStart`/
  `saslContinue`, same SCRAM math as the Postgres client), **doubles** (BSON 0x01,
  encode + decode), and **cursor pagination** — `mongo_find_all` now follows the cursor
  with `getMore`, so it returns *all* documents, not just the first batch. Also adds
  BSON binary (used for the SCRAM payloads). Verified against an authenticated `mongo:7`
  (login + a double + 250 docs across batches).
- **`f64_bits(float) -> int` / `f64_from_bits(int) -> float`** — reinterpret a double's
  IEEE-754 bits as an int64 and back, the byte-level access needed to (de)serialize
  64-bit floats (e.g. BSON doubles). This unblocked Mongo doubles.

## v0.66.0

- **MongoDB client** (`framework/mongo.src` + `framework/bson.src`) — a pure-MFL
  client speaking the **OP_MSG wire protocol** over `dial()`, with a **BSON codec**,
  no driver and no cgo. `bson.src` builds a document (`bson_new` + `bson_str`/`bson_i32`/
  `bson_i64`/`bson_bool`/`bson_null`/`bson_subdoc`/`bson_subarr` + `bson_finish`) and
  decodes one to a JSON string (`bson_to_json`). `mongo.src`: `mongo_connect`,
  `mongo_insert_one`, `mongo_find_all` (→ a JSON-array string, so `parse(docs, []T{})`
  decodes; `_id` ObjectId comes back as a hex string), `mongo_count`, `mongo_drop`,
  `mongo_close`. Verified against `mongo:7`. v1: unauthenticated local Mongo,
  int/string/bool/null/ObjectId (doubles decode to null), first-batch finds.

## v0.65.0

- **Connection pooling for the Postgres + Redis clients** — the gap the SaaS demo
  surfaced (machweb runs each request in its own goroutine, so a shared single
  connection would interleave). The clients are now **handle-based**: a connection is
  a `PgConn`/`RedisConn` (an fd + a one-element read-buffer box that persists across
  calls though the struct is a value). A **pool** is an async channel of authenticated
  fds (machin channels are unbounded queues → a natural semaphore, no new language
  feature needed): `pg_pool_init(n, …)` / `pg_acquire()` / `pgq`/`pgx`/`pg_release`,
  and `redis_pool_init(n, …)` / `redis_acquire()` / `r*` helpers / `redis_release`.
  Each acquired connection has its own buffer, so concurrent requests never interleave.
  The single-connection API (`pg_connect`/`pg_query`/`pg_exec`; `redis_connect`/
  `redis_set`/…) is unchanged (thin wrappers over one default connection). Verified:
  30 goroutines over 4 pooled connections, every result correct (gated tests); the
  [SaaS demo](https://github.com/javimosch/machin-saas-demo) handles 40 concurrent
  requests cleanly.

## v0.64.0

- **Redis client** (`framework/redis.src`) — a pure-MFL Redis client speaking the
  RESP protocol over `dial()`, no client library. `redis_connect` (+ `redis_auth`),
  typed helpers (`redis_set`/`redis_setex`/`redis_get`/`redis_del`/`redis_exists`/
  `redis_incr`/`redis_expire`/`redis_rpush`/`redis_lpush`/`redis_lrange`/`redis_keys`),
  and a general `redis_cmd(args []string) -> (val, ok)` (ok=0 on a `-error`/nil).
  Parses all five RESP types; array replies come back as a JSON-array string, so
  `parse(val, []string{})` decodes them. For cache, sessions, counters, rate-limits,
  and simple queues. Verified against `redis:7`, plus a CI-runnable RESP test against
  an in-process canned-reply mock. v1 is string values.

## v0.63.0

- **SSO — OAuth2 / OpenID Connect** (`framework/sso.src`): "log in with Google /
  Microsoft / …" in pure MFL on top of machweb's signed sessions. Fill an
  `OAuthProvider`, then `sso_begin(p, secret)` (302 to the provider + a signed
  CSRF-state cookie) and `sso_complete(p, secret, req) -> (profile, ok)` (verify
  state, exchange the code via `http_request`, fetch the userinfo JSON). Identity
  comes from the userinfo endpoint, so no JWT/RSA is needed. Verified end to end
  against an in-process mock IdP (token exchange + userinfo + CSRF rejection).
- **machweb: `redirect(url)`** (302 + `Location`) and **`query(req, name)`** (a
  `?`-query-string parameter, url-decoded) — both general-purpose, needed by SSO.
- **Compiler fix — an omitted string struct-literal field is now `""`, not a NULL
  pointer.** A string's zero value is `""`, but C zeroes an omitted compound-literal
  field to `NULL`, which crashed every string op (compare/concat/len/substr/print).
  `structLit` now fills omitted string fields (recursing nested structs) with `""`,
  and the string operators (`+`, `==`/`!=`) are NULL-tolerant as defense. Surfaced by
  the SSO dogfood: a machweb handler returning a `Response` with an unset `location`
  segfaulted on `!= ""`. Regression tests added.

## v0.62.0

- **Cookies + signed sessions in machweb** (`framework/machweb.src`) — the auth
  foundation. Request: `cookie(req, name)` reads a request cookie. Response (a value
  type — the helpers return a new `Response`): `set_cookie` / `clear_cookie` add a
  `Set-Cookie` with safe defaults (`Path=/; HttpOnly; SameSite=Lax`). Login sessions:
  `set_session(res, secret, name, value)` stores `value` + an HMAC-SHA256 tag the
  client can't forge, and `get_session(req, secret, name) -> (value, ok)` returns
  `ok == 1` only if it verifies (rejects a tampered tag or the wrong secret). The
  `Response` struct gained a `cookies []string` field; `machweb_handle` emits the
  `Set-Cookie` lines. Keep `secret` server-side; the value is signed, not encrypted.

## v0.61.0

- **Postgres parameterized queries** — `pg_exec(sql, []string params)` in
  `framework/postgres.src` runs the **extended query protocol**
  (Parse/Bind/Describe/Execute/Sync): the SQL uses `$1, $2, …` placeholders and
  params are bound server-side as text, never interpolated — **injection-safe**.
  Works for `SELECT` (returns JSON rows like `pg_query`) and for
  `INSERT`/`UPDATE`/`DELETE` (returns `[]`). `pg_query(sql)` remains for trusted/
  constant SQL. Verified against `postgres:16`, including an injection param that
  stays inert. This was the top backend-roadmap follow-up to the v0.60.0 client.

## v0.60.0

A pure-MFL **PostgreSQL client** — the first networked datastore, and the start of
the SME-backend dogfood (see `docs/NORTH-STAR-BACKEND.md`). Building it surfaced two
binary primitives, now added to the language.

- **`framework/postgres.src`** — a PostgreSQL client speaking wire protocol v3 over
  `dial()`, with **SCRAM-SHA-256** auth (the modern Postgres default), in pure MFL
  (no libpq, no cgo). `pg_connect(host, port, user, db, password)` / `pg_query(sql)` /
  `pg_disconnect()`. `pg_query` returns a **JSON-array-of-rows string in the same
  shape as `sqlite_query`**, so `parse(rows, []T{})` decodes it — numeric and bool
  columns come back unquoted (typed via the result's column OIDs), NULL → `null`.
  v1 is the simple-query protocol; `?`-parameter binding (extended protocol) is the
  next milestone.
- **`read_bytes(fd) -> bytes`** — NUL-safe socket read. `read()` returns a C string
  and truncates at the first 0 byte; a binary wire protocol needs the raw bytes.
- **`base64_encode_bytes(bytes) -> string` / `base64_decode_bytes(string) -> bytes`**
  — binary-safe base64 (the string forms stop at a NUL), for SCRAM salts/proofs and
  any binary token.
- SCRAM's XOR folds (PBKDF2, ClientProof) use MFL's native bitwise operators
  (`^ & | << >>`) — no new builtin needed.

## v0.59.1

Agent-discovery polish — fixes surfaced by dogfooding the north-star flow ("learn
machin and build a users back-office" from a cold start). No language changes.

- **`machin guide` clarified** where a fresh agent silently shipped a bug:
  `json_get` now states it returns the **raw JSON token** (a string field comes
  back **quoted**); `parse` documents its **slice witness** (`parse(jsonArray,
  []T{}) -> []T`); `sqlite_query` points at `parse(rows, []T{})` as the
  row-iteration idiom; a new `sqlite-rows-decode` gotcha ties it together.
- **`skills/machin-web/SKILL.md`**: fixed the wrong `sqlite_query -> []string`
  signature (it returns a JSON-array string); led the CRUD recipe with
  `parse(rows, []User{})`; documented the form-encoded POST-body path (use the
  `url_decode` builtin + a `form_field` helper — don't hand-roll `url_decode`,
  it's a builtin and shadowing it is a compile error).
- **New runnable example** `examples/complex/sqlite_crud.mfl` — the SQLite data
  layer end to end (the repo had no in-tree SQLite example before).
- **`AGENTS.md`** dogfood section now points web-building agents at the
  `machin-web` skill (previously only game-dev was signposted).

## v0.59.0

- **Fix: a closure can capture and mutate an aggregate local** (a slice, map, or
  struct). The heap box for such a captured local is now zeroed via `calloc`;
  codegen previously emitted `*box = {0}`, invalid C for an aggregate type (it
  compiled only for scalars). So a closure that captures `xs := []int{}` and
  `append`s to it across calls now works.
- **`framework/reactive.src` rewritten on the simpler model** the captured-closure
  fixes (v0.58.0 + this) unlock: a reaction is now just a **`func()` thunk that
  captures its own compute closure** and applies its effect, in one `[]func`
  registry — instead of typed per-kind arrays (`c_fn`/`b_fn`/`e_kf`/…) with a
  `run_computed`/`run_bind`/`run_each` dispatch. `bind` captures its `last`, `each`
  its `old` key set, `computed` is a signal kept current by a reaction. Same public
  API, **byte-identical patch behavior** (verified), ~9 fewer globals, the dispatch
  gone. (Re-confirmed gotcha: a parameter named like a builtin — `keys` — is
  shadowed at call sites; `each`'s param was renamed `keyfn`.)
- **`framework/router.src` — a client-side router** (composes with reactive, enabled
  by the cleaner `reaction()` primitive). The active route is a signal (an int
  index): `route(path)` registers pages, `navigate(i)` / `nav(path)` switch,
  `link(path, label)` renders an anchor, and `outlet(id, render)` re-renders the
  active page (a reaction over a new `dom_html` host import) and syncs the address
  bar (`nav_url`). `nav` takes the path string from the host via `ptr_str`. For
  multi-page admin apps / SPAs.

## v0.58.0

- **Fix: a captured closure can be CALLED inside a lambda.** `func(){ fn() }` where
  `fn` is a captured variable now works — previously it failed to compile (`call to
  undefined function`), because closure conversion's free-variable scan ignored a
  call's *callee*, so the closure was never captured. This is the higher-order
  function building block: functions that return closures calling their arguments,
  and the clean reactive shape `effect(func(){ compute() })` (a stored `func()`
  thunk that calls a captured compute). Top-level functions/builtins are unaffected
  (they were never in the enclosing scope, so they're still not captured). One-line
  fix in `freeIdents`; no regressions.

## v0.57.0

- **`ptr_str(ptr) -> string` — host→wasm strings (text input / forms).** Reads a
  NUL-terminated string out of raw memory into an MFL (arena) string. This is the
  missing **into-wasm** string direction: the JS host writes UTF-8 + a NUL into the
  module's memory at a pointer the program `alloc`'d, then calls an export passing
  that pointer, and the program reads it with `ptr_str`. (Strings already flowed
  *out* of wasm as a memory pointer the host decodes; ints flow both ways.) Pairs
  with `alloc`/`free`. Verified round-trip incl. multi-byte UTF-8 (accents, emoji).
  Unblocks forms — an `<input>`'s text can now reach a machin component.

  ```go
  export func input_buf(n) (p) { p = alloc(n + 1) }     // host writes n bytes + NUL here
  export func submit(p) { add_todo(ptr_str(p))  free(p) }
  ```

## v0.56.0

- **`framework/reactive.src`: `hydrate` + a value-embedding `slot` — isomorphic
  apps.** A server can SSR-render a component's markup and the wasm client now
  attaches its reactivity to that **existing DOM** instead of re-rendering it:
  - **`slot(name, compute)`** now embeds the current value in its markup
    (`<span data-s="name">VALUE</span>`), so `mount` paints filled markup (no
    first-paint flash) and so SSR markup and client markup agree.
  - **`hydrate(html)`** flushes the queued bindings/lists **without** a `dom_mount`
    — it connects them to the SSR elements by their `data-s` / container names.
    `mount` is now `dom_mount` + `hydrate`.

  So one component renders on the server (first paint, works with JS off) and
  hydrates in the browser with no re-render. Drove
  [boilerplate-cli-ui-machin-isomorphic](https://github.com/javimosch/boilerplate-cli-ui-machin-isomorphic)
  — a single binary that is a CLI + HTTP server + JSON API + reactive wasm UI, the
  full-stack-MFL capstone.

## v0.55.0

- **`framework/reactive.src` gains a templating layer** — a component declares its
  markup *and* its reactive bindings in one place, instead of a hand-written HTML
  skeleton plus manual `data-s` wiring:
  - **`slot(name, compute)`** returns the markup for a reactive text node and
    queues its binding.
  - **`list(name, keys, item)`** returns the markup for a keyed-list container and
    queues its reconciler.
  - **`mount(root, html)`** sets the root element's HTML once (via a new
    `dom_mount` host import), then flushes the queued bindings/lists so they paint
    after their elements exist.

  So a whole component is one expression: `mount("app", "<h1>…</h1>" + slot("count",
  fn) + list("items", keys, item))`. Static markup is plain string concatenation;
  the JS host shrinks to a `dom_mount` + the patch ops. Pure MFL (no compiler
  change). Updated [machin-web-demo-reactive](https://github.com/javimosch/machin-web-demo-reactive)
  to generate its own markup — `index.html` is now just `<div id="app"></div>`.

## v0.54.0

- **`framework/reactive.src` grows computed signals + keyed list reconciliation.**
  - **`computed(func(){ return … })`** — a memoized derived signal (backed by a
    real signal, kept current by a reaction); read it with `get` and depend on it
    like any signal. Transitive: a change recomputes the computed, which updates
    its own dependents.
  - **`each(container, keys, item)`** — keyed list reconciliation. `keys()` returns
    the ordered keys as a CSV string (`func(){ get(ver)  return csv(ids) }`),
    `item(key)` returns an item's HTML. On a change it emits only the deltas —
    `list_insert` for new keys (rendered once), `list_remove` for gone keys, and a
    `list_order` directive — never re-rendering unchanged items. The unified
    reaction graph means a computed/binding/list all recompute only when a signal
    they read changes, and only changed text/keys patch.
- **Defining a function named like a builtin is now a compile error** (it would be
  silently shadowed by the builtin at call sites — a footgun that bit the reactive
  runtime three times: `flush`, `keys`, `contains`). Rename it. An `extern` may
  still shadow a builtin (intentional, for FFI). Drove
  [machin-web-demo-reactive](https://github.com/javimosch/machin-web-demo-reactive)
  (now a list + computed demo).

## v0.53.0

- **Slices of functions — `[]func`.** A slice literal may now have `func` as its
  element type (`fns := []func{}`), so closures can be stored, appended, indexed,
  and called from a slice — the dispatch-table / callback-list / effect-registry
  primitive. (Each element is an `mfl_closure`; the slice machinery already handled
  by-value structs, so this was a one-token parser fix.)
- **`framework/reactive.src` — a fine-grained reactive runtime for web UIs** (the
  Solid/Leptos model, in MFL). Built on `[]func` (a binding registry of compute
  closures) and package globals (the signal store): **signals** hold state
  (`signal`/`get`/`set`), **bindings** are compute closures tied to a DOM slot
  (`bind(slot, func(){...})`) whose signal reads are auto-tracked as dependencies,
  and on `set` only the bindings that read that signal recompute — emitting a
  **patch** (`dom_patch(slot, value)`) only when the rendered text actually
  changed. The host sets the text of the handful of changed slots; no innerHTML
  replacement, no vdom diff. Drove [machin-web-demo-reactive](https://github.com/javimosch/machin-web-demo-reactive).
  - Gotcha it surfaced: a user function named like a builtin (e.g. `flush`) is
    silently shadowed by the builtin — the runtime's internal `commit` was renamed
    to avoid it. Lambda **named returns** aren't supported yet (`func() (s) {…}`);
    use `func() { return … }`.

## v0.52.0

- **Package-level variables — `var name = expr` at top level.** A mutable global
  shared by every function, its type inferred from the initializer (and its uses).
  Unlike a local it **persists across calls**, so a wasm module's exported
  functions can finally hold state between host invocations — `var count = 0` +
  `export func bump(d) { count = count + d }` accumulates across calls. The piece
  that was missing for a component to own its state in machin (not the JS host).
  - `=` to a global's name assigns the global; `:=` still introduces (and may
    shadow with) a local. Globals are visible everywhere, including inside
    closures — a captured global is referenced directly, not closed over.
  - Works for any type: scalars, strings, **maps** (`var hits = make(map[string]int)`,
    then `hits[k] = …`), and **slices** (`var log = []string{}`, then `append`).
  - Implementation: each global is a C static `mfl_g_<name>`; the initializers run
    in a `constructor` — before `main` (native) and at `_initialize` (wasm reactor).
    `encode` keeps a brace-less top-level `var` as its own declaration.

## v0.51.0

- **Binary HTTP bodies — a machweb server can serve its own wasm (and any binary
  asset).** Two NUL-safe builtins over the `bytes` type:
  - **`read_file_bytes(path) -> bytes`** — read a whole file's raw bytes (the
    existing `read_file` returns a C string and truncates at the first NUL, so it
    can't carry a `.wasm`/image/font).
  - **`write_bytes(fd, bytes) -> int`** — write the exact bytes of a buffer to an
    fd (a full-write loop), unlike `write` which `strlen`s a string body.

  [`framework/machweb.src`](framework) gains a binary response path: the `Response`
  carries an optional `bin bytes` (+ `is_bin` flag), with builders **`ok_bytes(ctype,
  b)`** and **`ok_wasm(b)`**; `machweb_handle` writes the headers then the raw bytes
  when `is_bin` is set. So a single native machin binary can ship both its SSR HTML
  *and* its own `.wasm` SPA bundle — verified byte-identical over HTTP, and the
  served module still instantiates. The keystone for **full-stack MFL** (one binary,
  server-rendered then hydrated); drove
  [machin-web-demo-ssr](https://github.com/javimosch/machin-web-demo-ssr). `write_bytes`
  is gated like the rest of the socket runtime (native always; wasm only when used).

## v0.50.0

- **`--target wasm` — machin compiles to WebAssembly (frontend / in-browser).**
  `machin build app.mfl --target wasm` cross-compiles the emitted C to a
  `wasm32-wasi` **reactor** module via `zig cc` (zig bundles clang + a wasi-libc,
  so it is a single-binary C→wasm toolchain — no emscripten/wasi-sdk; override
  with `ZIG=`). The module loads in a bare browser; the bridge is machin's own
  **FFI, both ways**:
  - **`export func name(...)`** — a new keyword that marks a function as a wasm
    **export** (and a reachability root, kept even if `main` never calls it). The
    host calls `instance.exports.name(...)`; the export carries an `export_name`
    attribute so JS sees the clean source name, not the mangled C symbol. A wasm
    module needs **no `main`** — an export is its own entry point.
  - **`extern "env" { fn set_html(string) }`** — under the wasm target a headerless
    extern becomes a wasm **import** (`import_module`/`import_name`) the host (JS)
    supplies, e.g. a DOM call. The `extern "<lib>"` name is the import module
    (default `env`).
  - Marshaling: machin ints are i64 ⇒ `BigInt` across the boundary; strings cross
    as a pointer into the exported `memory` (decode NUL-terminated UTF-8 host-side).
- **The POSIX socket/tty runtime is now pay-as-you-go.** The networking
  (`listen`/`accept`/`dial`/`read`/`write`/`close`) and terminal
  (`raw_mode`/`read_key`) runtimes are split out of the always-on core. The
  **native** target is unchanged (it always carries them, and still emits `int
  main`); the **wasm** target emits each only when the program actually uses it,
  so a frontend module references no `socket()`/`termios` symbols (which wasi-libc
  doesn't fully provide) and compiles clean — the same `usesX` gating machin
  already does for TLS/WebSocket/regex/math/SQLite. Drove
  [machin-web-demo-wasm](https://github.com/javimosch/machin-web-demo-wasm); see
  `docs/NORTH-STAR-WEB.md`. Still ahead for a richer frontend: package-level
  globals (state lives host-side today) and a shipped JS string/array runtime.

## v0.49.0

- **`noise2` / `noise3` — native Perlin gradient noise.** Deterministic, ~`[-1,1]`,
  smooth/continuous; 2D and 3D. The backbone of procedural worlds — layer it (fbm,
  in MFL) for terrain, animate 2D noise over time with `noise3`. Pure C + libm's
  `floor`; the runtime is emitted and `-lm` linked only when used. Drove a full
  procedural planet ([machin-game-demo-cyberpunk](https://github.com/javimosch/machin-game-demo-cyberpunk)):
  infinite chunk-streamed terrain + procedurally placed buildings, all from noise.

## v0.48.0

- **Pointer-bearing `cstruct` fields + inout `T*` params.** Two follow-ups to the
  v0.47.0 pointer/array FFI that let MFL declare and pass C structs containing
  pointers, instead of poking raw bytes at hard-coded offsets:
  - a `cstruct` **field** may be **`ptr`** — held as an `int` in MFL, cast through
    `void*` at the boundary (C converts to `float*`, `unsigned char*`, …). So a
    struct like raylib's `Mesh` is declared with its pointer fields and the C
    compiler lays it out.
  - a new **inout** param form **`T*`** (`T` a declared `cstruct`) — the arg is a
    cstruct *variable*, marshaled to a C temporary, passed **by pointer**, and the
    modified struct **written back** afterward (e.g. `fn UploadMesh(Mesh*, bool)`
    returns the GPU vao/vbo ids in the mesh).

  Together these drop the hard-coded `Mesh` byte offsets from
  [machin-game-demo-planet](https://github.com/javimosch/machin-game-demo-planet): the GPU
  mesh is now a `cstruct Mesh { … vertices ptr colors ptr … }` built by value and
  uploaded via the inout `Mesh*`. Resolves the rough edge noted in v0.47.0.

## v0.47.0

- **Pointer/array FFI — raw memory + `*T` params.** machin can now build C
  buffers and structs and hand them to a foreign API:
  - raw heap memory (pointers are `int`s): `alloc(n)` (zeroed), `free(p)`,
    `poke_f32`/`poke_i32`/`poke_u8`/`poke_u16`/`poke_ptr(p, byteOffset, v)`,
    `peek_f32`/`peek_i32(p, byteOffset)`.
  - a new FFI param convention **`*T`** — the MFL arg is a pointer (`int`); the
    call dereferences it and passes the pointed-to C struct **by value** (e.g.
    `fn LoadModelFromMesh(*Mesh) Model`). Pass the pointer itself with `ptr`
    (`void*` → any `T*`), so an in/out `UploadMesh(Mesh*)` writes back into the
    buffer.
  - Also: an explicit `extern` declaration now resolves before the builtin
    switch (introduced in v0.46.0), so reaching a foreign `fn` of the same name
    as a builtin still works.

  This is the first FFI tier that hands C **raw pointers/arrays**, unlocking GPU
  vertex buffers (`UploadMesh`/`LoadModelFromMesh`/`DrawModel`) — a procedurally
  generated mesh built in MFL and uploaded to VRAM. Surfaced (and verified) by
  [machin-game-demo-planet](https://github.com/javimosch/machin-game-demo-planet). The
  Tier-2 unlock in the [game-dev north star](docs/NORTH-STAR-GAMEDEV.md).

## v0.46.1

- **docs:** fix the guide's `ffi-nested-cstruct` gotcha, which still said "no
  native sin/cos/sqrt yet" after v0.46.0 added them; point to the `math` builtins
  instead. Refresh the `machin-gamedev` skill's math note + dogfood record.

## v0.46.0

- **Native math builtins.** A floating-point math suite over libm:
  `sin cos tan asin acos atan atan2 sqrt cbrt pow exp log log2 log10 floor ceil
  round trunc abs fmod hypot` and `pi()`. Numeric in, `float` out; libm is linked
  (`-lm`) and the runtime emitted **only when a math builtin is used**, so
  math-free programs keep their libc-only footprint. An `extern` declaration of
  the same name still shadows the builtin (so existing `extern "m" { fn sqrt ... }`
  code is unchanged). Surfaced by [machin-game-demo-3d](https://github.com/javimosch/machin-game-demo-3d),
  which had to reach libm via `extern "m"` for its camera orbit — now `sin`/`cos`
  are native. The driver for procedural-animation apps.

## v0.45.0

- **FFI nested cstructs.** A `cstruct` field may now be another declared
  `cstruct`, not just a numeric scalar — a by-value struct of by-value structs.
  The synthesized MFL struct nests the inner `mfl_` type and the boundary
  marshaling recurses (`mfl_from_`/`mfl_to_` per field). This unblocks **3D**:
  raylib's `Camera3D` is three `Vector3`s + scalars, so it couldn't be expressed
  before; now `Camera3D{Vector3{...}, Vector3{...}, Vector3{...}, 45.0, 0}`
  constructs and passes by value to `BeginMode3D`. Surfaced building
  [machin-game-demo-3d](https://github.com/javimosch/machin-game-demo-3d). (Also unlocks 2D
  cameras and any struct-of-structs C API.) Note the orbit math there still goes
  through libm via `extern "m"` — machin has no native `sin`/`cos`/`sqrt` yet,
  the next gap.

## v0.44.0

- **FFI opaque handles — `cstruct Name {}`.** An empty-body `cstruct` declares a
  by-value C type (from the `header`) that machin holds and passes back **without
  naming its fields**. This is for by-value structs that contain pointers and so
  can't be a numeric `cstruct` — e.g. raylib's `Sound`/`Music`/`Font`. MFL can
  receive one from a `fn`, store it (a variable or `[]Name`), and pass it to
  another `fn`; it can't construct or field-access it. machin wraps the real C
  struct in one hidden field and copies it whole at the boundary, so the existing
  by-value marshaling path carries it. Surfaced building
  [machin-game-demo-simon](https://github.com/javimosch/machin-game-demo-simon), whose audio
  needs `LoadSound`→`PlaySound` over raylib's pointer-bearing `Sound`. Unlocks
  every "load a handle, pass it back" C library, not just audio.

## v0.43.0

- **`float()` — int → float conversion.** The counterpart to `int()`. MFL has no
  implicit `int`→`float`: only a flexible numeric *literal* promotes against a
  float; a *concrete* int (a function return, `byte_at`, `len`, a typed param, an
  `int`-slice element, or an `f32`/`f64` FFI struct field) was a hard
  `int vs float` mismatch. `float(x)` lifts it. Surfaced building the physics and
  random-pipe placement in [machin-game-demo-flappy](https://github.com/javimosch/machin-game-demo-flappy),
  where `byte_at`-derived randomness and pixel coordinates are all float.

## v0.42.0

- **`str` accepts `bool` and `string`.** `str(true)` → `"true"`, `str(false)`
  → `"false"`, and `str` of a string is the identity. `str` was numeric-only, so
  `"moved=" + str(moved)` was a `bool vs num` type error — a papercut everyone
  worked around with a hand-written `b2s` helper. Surfaced repeatedly building
  the game logic in machin-game-demo-snake / machin-game-demo-2048. Non-stringable kinds
  (slice, map, struct, …) are still a clean compile error.

## v0.41.0

- **Terminal input: `raw_mode(on)` + `read_key()`.** Real-time, per-keypress
  terminal input. `raw_mode(1)` puts the tty in cbreak/no-echo mode (and
  `raw_mode(0)` restores it); `read_key()` is a non-blocking single-key read —
  a 1-char string, or `""` if nothing is waiting. `input()` was line-buffered
  (it blocks for a whole line + Enter), which interactive TUIs and games can't
  use. Surfaced by [machin-game-demo-snake](https://github.com/javimosch/machin-game-demo-snake)
  — and it unlocks every future terminal UI (pickers, progress views, REPLs).

## v0.40.0

- **Bitwise operators + hex/binary/octal literals.** `& | ^ << >>` (and unary `^`,
  complement), `int`-only, with Go's precedence (`<< >> &` bind like `* / %`;
  `| ^` like `+ -`). Integer literals now accept `0xff`, `0b1010`, `0o17` (with
  `_` separators). The whole binary/crypto/protocol surface (machin-protobuf,
  machin-wabin, machin-signal, machin-noise) had been faking these with `* / %`
  over powers of two — `cb >> 4 & 0x0f` instead of `(cb / 16) % 16`. Surfaced by
  that accumulated real usage.

## v0.39.0

- **The HTTP client now does plain `http://`, not just TLS.** `http_get`,
  `https_get`/`https_post`, and `http_request` previously rejected `http://` with
  `err="scheme"`; now they connect over a plain TCP socket for `http://` URLs
  (default port 80) and TLS for `https://` (443), sharing the same
  request/redirect/chunked/Content-Length handling — so `http→https` redirects
  follow transparently. Surfaced building machin-watch (an uptime monitor wants
  to watch plain-HTTP endpoints).

## v0.38.0

- **`xeddsa_sign` / `xeddsa_verify` builtins — XEdDSA (Curve25519 signatures).**
  The signature scheme Signal/WhatsApp use for identity and device signatures
  (signing a Curve25519 key, not plain Ed25519). Backed by libsodium's Ed25519
  group/scalar ops + OpenSSL SHA-512 + TweetNaCl field arithmetic for the
  Montgomery→Edwards conversion; matches libsignal's `SignCurve25519.go`. Emitted
  and linked (`-lsodium -lcrypto`) only when used — **requires libsodium-dev** on
  the build host. Surfaced building WhatsApp device pairing (machin-wapair).

## v0.37.0

- **`bytes` is now a first-class declarable type.** It was inference-only (locals
  from `bytes()`/crypto builtins); now it's usable in `struct` fields, `map`
  values (`map[string]bytes`), and `[]bytes`, so you can hold binary state in a
  record — needed for protocol state machines (e.g. a Noise handshake). One-line
  type-checker change; the C type (`mfl_bytes`) already existed.

## v0.36.0

- **Binary WebSocket frames: `wss_send_bin(conn, bytes)` / `wss_recv_bin(conn) -> bytes`.**
  The existing `wss_send`/`wss_recv` are text (`char*`) and truncate at a NUL; the
  binary variants carry a `bytes` payload (send as opcode `0x2`, recv NUL-safe).
  The frame loop is refactored into a shared core, so text and binary recv behave
  identically (ping/pong, fragmentation, close). Step 3 of the native-WhatsApp path
  (the protocol is binary framing).

## v0.35.0

- **Crypto builtins over `bytes` (OpenSSL libcrypto).** Step 2 of the native-crypto
  path: `rand_bytes`, `sha256_bytes`, `hmac_sha256_bytes`, `hkdf_sha256`,
  `x25519_pub`/`x25519_shared`, `ed25519_pub`/`ed25519_sign`/`ed25519_verify`,
  `aes_gcm_encrypt`/`decrypt`, `aes_cbc_encrypt`/`decrypt` — thin wrappers over
  OpenSSL, all operating on `bytes`. Emitted and linked (`-lcrypto`) only when a
  program uses one, so crypto-free programs stay lean. This proves the viability
  checkpoint: machin can do an X25519 ECDH handshake natively. (Digests match
  OpenSSL byte-for-byte; X25519 agreement, Ed25519 sign/verify, and AES-GCM/CBC
  round-trips all verified.)

## v0.34.0

- **`bytes` type — a NUL-safe binary buffer.** machin strings are NUL-terminated
  `char*`, so they can't hold arbitrary binary (anything with a `0x00` byte gets
  truncated). The new `bytes` type (pointer + length) can. Builtins: `bytes(str)`,
  `bytes_str(b)`, `to_hex`/`from_hex`, `byte_at`, `bytes_sub`, `bytes_concat`;
  `len(b)` works, and `println(b)` prints hex. This is the foundation for binary
  protocols and real crypto (step 1 of a native WhatsApp client — see machin-meet).

## v0.33.0

- **`http_request(method, url, headers, body)` builtin.** Authenticated HTTPS for
  any method, with caller-supplied header lines — the piece `https_get`/`https_post`
  lacked (they hard-code the header set, so no `Authorization`). `headers` is a
  `[]string` of `"Key: Value"` lines; returns `(status, body, err)` like `http_get`.
  Surfaced wiring WhatsApp booking notifications into machin-meet (the WhatsApp
  Cloud API and Twilio both require a bearer/basic `Authorization` header).

## v0.32.0

- **`url_encode` / `url_decode` builtins.** Percent-encoding for URLs (RFC 3986):
  `url_encode` keeps the unreserved set `A-Za-z0-9-._~` and `%XX`-encodes the rest
  (space → `%20`); `url_decode` reverses it leniently (`+` → space, malformed `%XX`
  passes through). Surfaced building machin-qs (a query-string ⇄ JSON converter),
  and lets servers like machin-meet decode query/form values safely.

## v0.31.0

- **`time_format_utc(unix, fmt)` builtin.** Like `time_format` but in UTC
  (`gmtime` instead of `localtime`) — the form iCalendar `.ics` and RFC-3339
  timestamps want, without the `%z`-offset arithmetic dance. Surfaced finishing
  machin-meet, whose `.ics` `DTSTART`/`DTEND` must be in UTC.

## v0.30.0

- **`time_make(y, mo, d, h, mi, s)` builtin.** Build a Unix timestamp from local
  calendar fields — the inverse of `time_fields`, backed by `mktime(3)` (which
  also normalizes out-of-range fields, so day 32 rolls into the next month). This
  completes the time trio: construct ↔ decompose ↔ render. Surfaced building
  machin-meet (a one-person self-hostable Calendly), which needs "09:00 local on
  date X → which Unix second?" to enumerate bookable slots.

## v0.29.0

- **`time_format(unix, fmt)` builtin.** Format a Unix timestamp (local time) with
  a `strftime(3)` pattern — `%Y-%m-%d`, `%H:%M:%S`, weekday/month names (`%A`/`%B`),
  zone name/offset (`%Z`/`%z`), `%F`, `%T`, and the rest. The pieces `time_fields`
  can't give you (locale names, zone). Surfaced building machin-date (a `date(1)` clone).

## v0.28.0

- **`time_fields(unix)` builtin.** Decompose a Unix timestamp (local time) into
  `[year, month, day, hour, minute, second, weekday(0=Sun), yearday]` — the
  calendar view machin lacked (it had `now()` but no way to read its parts).
  Backed by `localtime_r`. Surfaced building a cron-expression evaluator.

## v0.27.0

- **Parameterized SQLite queries.** `sqlite_exec` and `sqlite_query` now take an
  optional third argument — a `[]string` whose values bind to the `?`
  placeholders, in order (via `sqlite3_bind_text`). This is **injection-safe**: a
  value containing SQL is stored/compared literally, never executed. The two-arg
  forms are unchanged.

## v0.26.0

- **SQLite builtins — `sqlite_open`, `sqlite_exec`, `sqlite_query`, `sqlite_close`.**
  Real database storage, backed by `libsqlite3`. `sqlite_open(path)` returns a
  handle (`:memory:` for in-memory); `sqlite_exec` runs result-less SQL;
  `sqlite_query` runs a SELECT and returns a **JSON array of row objects**
  (INTEGER/REAL unquoted, TEXT escaped, NULL null) — so it composes with
  `json_get`. Emitted and linked (`-lsqlite3`) only when a program calls
  `sqlite_*`. Surfaced building a persistent key-value store.

## v0.25.0

- **`read_stdin()` builtin.** Reads all of stdin verbatim until EOF — exact
  bytes, no line splitting — unlike the line-based `input()` (which strips
  newlines and loses the trailing-newline distinction). This is what lets a tool
  process its input byte-exact (an exact byte count, a precise webhook body, a
  binary-ish payload). Surfaced building a `wc` clone.

## v0.24.0

- **Hash builtins — `sha256`, `hmac_sha256`.** `sha256(s)` and
  `hmac_sha256(key, msg)` return a lowercase hex digest. Pure C (no dependency),
  byte-exact against `sha256sum`/`openssl` (FIPS-180-4 + RFC 2104 test vectors).
  The common use is verifying webhook signatures (GitHub `X-Hub-Signature-256`,
  Stripe). Surfaced building a webhook signature verifier; completes the
  decode-then-verify story machin-jwt started.

## v0.23.0

- **Base64 builtins — `base64_encode`, `base64_decode`.** `base64_encode` emits
  standard padded base64; `base64_decode` is lenient — it accepts the standard
  and url-safe alphabets (`-`/`_`) and ignores padding/whitespace, so it also
  decodes JWT segments. Pure C (no dependency), in the always-on runtime.
  Surfaced building a JWT decoder.

## v0.22.0

- **Regex builtins — `regex_match`, `regex_find`, `regex_groups`, `regex_replace`.**
  POSIX extended-regex (ERE) over the subject string: test a match, extract the
  first match, pull capture groups (`[0]` whole, `[1..]` subgroups), or replace
  all matches. Backed by libc's `<regex.h>`, emitted only when a program uses
  `regex_*` (so others stay portable). Surfaced building a grep.

## v0.21.0

- **Left-to-right evaluation order (fixes #142).** Operands and arguments now
  evaluate in source order, matching Go — previously machin inherited C's
  unspecified order, so `f() + g()` could run `g()` first. Codegen hoists
  side-effecting sub-expressions into sequenced temporaries (a GNU statement-
  expression) for binary ops, call arguments, slice/struct literals, and
  multi-return lists; pure expressions are untouched (no overhead). The
  `eval-order` note in `machin guide` now states the guarantee.

## v0.20.0

- **`machin guide` completeness pass.** A fresh-eyes audit confirmed the builtin
  (51) and keyword catalogs match the compiler exactly, and filled the gaps: new
  idioms for the *functions* surface (`variadic`, `named-returns`, `closure`,
  `generic`, `scoped-arena`) and new gotchas — struct **value semantics** (copied
  on pass; use a map for shared mutable state), **no map comma-ok** (`v, ok :=
  m[k]` doesn't compile), the `parse(s, T{})` **witness**, and **unspecified
  evaluation order** (the review found `f() + g()` runs right-to-left, unlike Go;
  tracked in #142). Now 14 idioms, 13 gotchas, all compiled by a test.
- **`framework/flags.src` — a CLI flag parser (MFL module).** Every machin tool
  hand-rolled its argument parsing; this is a reusable parser composed like
  `machweb` (`machin encode framework/flags.src yourtool.src`). Short/long flags,
  the `=` and space value forms, bool flags, defaults, positionals, typed getters,
  and an auto `--help`. Its value store uses maps (reference types) so updates
  survive the `Flags` struct being passed by value — no compiler change. Drove
  [machin-http](https://github.com/javimosch/machin-http) (get/post/head). Closes #138.

## v0.19.0

- **`machin guide` — self-describing feature catalog for agents.** One command
  emits machin's complete, version-exact surface — keywords, types, every builtin
  with its signature + one-line semantics (grouped by category), the core idioms
  as runnable snippets, and the gotchas — as **JSON by default** (`--text` for
  dense prose). It's generated from a single in-binary source-of-truth catalog,
  so an agent masters the language in one call and the reference can't drift from
  the implementation: a test asserts every catalogued builtin is recognized by
  the compiler, and that the catalog version matches the README badge.

## v0.18.0

- **`flush()` builtin.** Forces buffered stdout out (`fflush`). libc fully buffers
  stdout when it's a pipe, so a streaming program's output otherwise only appears
  when the buffer fills or the process exits; calling `flush()` after a write
  makes it visible immediately downstream. Surfaced by the streaming batcher,
  whose whole point is timely emission.

## v0.17.0

- **Comma-ok receive — `v, ok := <-ch`.** A receive now optionally reports
  whether the channel is still open: `ok` is `false` once it's closed and
  drained (and `v` is the zero value). Works standalone and as a `select` case
  (`case v, ok := <-ch:`). Relatedly, **`select` now treats a closed channel as
  ready** — its receive case fires (with `ok == false` if bound) instead of
  spinning — so a `select` loop can detect a source closing. Built on the
  existing `mfl_chan_recv2` plus a new `mfl_chan_tryrecv2`. Surfaced building a
  stream batcher that flushes on size, on a timer, or when the input ends.

## v0.16.0

- **Channels deep-copy slices and maps too.** v0.15.0 made channels safe for
  strings; now slices, maps, and structs containing them (nested arbitrarily)
  are deep-copied across the goroutine boundary as well, so a `chan []string`,
  `chan map[string]int`, or `chan SomeStruct{…[]T…}` value sent from a short-
  lived goroutine survives that goroutine's arena being reclaimed. Plain strings
  keep the fast offset-copy path; elements containing a slice or map round-trip
  through the per-type JSON serializer/parser (a general deep copy reused from
  `json`/`parse`). Scalars are still a plain memcpy.

## v0.15.0

- **Fix: strings sent over a channel survive the sender goroutine.** A channel
  copied only the element bytes — for a string, just the `char*` — so a string
  allocated inside a short-lived goroutine and sent over a channel dangled once
  that goroutine's arena was reclaimed (garbled/corrupt reads on the far side).
  Channels are now string-aware: `make(chan T)` records the byte offsets of every
  string reachable by value in `T` (a bare `string`, or a struct's string fields,
  recursing into nested structs); send **deep-copies** those strings into stable
  storage, and receive **adopts** them into the receiver's arena (freeing the
  intermediate — no leak). Scalars are unaffected; slice/map backings inside an
  element are still shared (documented). Surfaced by machin-pipe, which had to
  work around it by keeping inputs in main's arena.

## v0.14.0

- **Channel `close` + range-over-channel.** Channels could be made and used but
  never closed, so a consumer had no clean "no more data" signal — pools stopped
  via sentinel values and a stray `range`/receive blocked forever. `close(ch)`
  now marks a channel done (waking every blocked receiver); a receive drains the
  buffer then yields the zero value, and **`for v := range ch`** loops until the
  channel is closed and drained. `close` dispatches on its argument — a channel
  closes the channel, an fd still closes the fd. Built on a new `mfl_chan_recv2`
  (receive-with-ok) primitive. Surfaced building a streaming fetch pipeline whose
  stages terminate by closing their channels.

## v0.13.0

- **`select` — wait on multiple channels.** machin had goroutines and channels
  but no way to wait on more than one at a time, so timeouts, cancellation, and
  worker-pool collectors were impossible. `select { case v := <-ch: ... case ch
  <- x: ... default: ... }` takes the first ready case (receives tried before
  sends, in source order), runs `default` when nothing is ready, or blocks when
  there's no default. Implemented as a poll over the cases using a new
  non-blocking `mfl_chan_tryrecv` primitive; case bodies run outside the poll
  loop so `break`/`continue`/`return` affect the enclosing scope. Surfaced
  building a bounded worker pool that races results against a deadline.

## v0.12.0

- **JSON path queries — `json_get(json, path)`.** Every machin tool used to dig
  into JSON with fragile substring search. `json_get` walks a jq-style path
  (`.key`, `[index]`, chained — `.a.b[0].c`, `.` for the whole document) and
  returns `(value, err)`: `value` is the located value's raw JSON text, `err`
  is `""`/`"notfound"`/`"path"`/`"parse"`. It's a non-allocating scanner that
  respects nesting and string escapes (no tree built), and the second builtin to
  use the `value, err :=` convention. Surfaced building a `jq`-style query CLI.

## v0.11.0

- **Error handling reaches the builtins — `http_get` returns `(status, body, err)`.**
  machin's HTTP builtins collapsed every failure to `""`: a 404, a 503, an
  empty-but-OK body, and an unreachable host were indistinguishable, so a program
  couldn't *handle* errors. `http_get(url)` brings the Go-style `value, err :=`
  idiom to the builtin layer — `status, body, err := http_get(url)`, where a
  non-empty `err` is a transport failure (`"dns"`/`"connect"`/`"tls"`/`"scheme"`)
  and otherwise `status` is the real HTTP code. The multi-assign destructure path
  now recognizes multi-return builtins; the existing `https_get`/`https_post`
  (body-only) are unchanged, both now built on the same status-aware core.
  Surfaced building a link checker that has to classify why a URL is broken.
- **`exit(code)`.** Terminate the process with a status code — so a CLI can fail
  CI on a bad result (the link checker exits non-zero on a broken link).

## v0.10.0

- **Native WebSocket — `wss_open`, `wss_send`, `wss_recv`, `wss_close`.** A
  `wss://` client (RFC 6455) over real TLS, no subprocess. `wss_open(url)` does
  the HTTP/1.1 Upgrade handshake and returns a connection handle; `wss_send`
  masks and writes a text frame; `wss_recv` blocks for the next message,
  reassembling fragments and transparently answering pings and handling close;
  `wss_close` tears down. Built on a shared TLS core refactored out of the HTTPS
  client (one process-global `SSL_CTX`), emitted and linked (`-lssl -lcrypto`)
  only when used. Surfaced dogfooding a streaming scraper that had to shell out
  to `websocat` — this retires that crutch too: a Polymarket CLOB stream now runs
  fully native (`https_get` to resolve the token, `wss_*` to stream).

## v0.9.0

- **Native TLS — `https_get` and `https_post`.** machin's biggest networking
  gap is closed: an HTTPS client over real TLS (OpenSSL), no subprocess. `https_get(url)`
  and `https_post(url, jsonBody)` return the response body, handling cert
  verification (SNI + hostname), `Content-Length`, chunked transfer-encoding, and
  redirects. The OpenSSL runtime is emitted and linked (`-lssl -lcrypto`) **only
  when used**, so TLS-free programs keep their libc-only footprint. Surfaced
  building a Polymarket scraper that had to shell out to `curl`/`websocat` because
  machin couldn't open a TLS socket — this retires the `curl` crutch for REST.

## v0.8.0

More dogfooding: building a streaming WebSocket scraper drove these in.

- **`break` and `continue`.** Loop control was missing entirely — the only way
  out of a `for`/`while` was a flag variable. `break` exits the innermost loop,
  `continue` skips to its next iteration; both work in `for cond`, `for {}`, and
  `range` loops (range increments live in the C `for` clause, so `continue` is
  safe). Surfaced writing hand-rolled JSON/stream parsers in MFL.
- **`encode` — string- and comment-aware function splitting.** `splitFunctions`
  counted every `{`/`}` to find declaration boundaries, including braces inside
  string literals and `//` comments. Any function emitting JSON (`"{...}"`) or
  searching for a brace (`index(s, "}")`) failed with `unbalanced braces`. It now
  tracks string state and stops at `//`.

## v0.7.0

Dogfooding: real tools drove these in. A health checker added networking +
timing + parsing; a static-site generator added file I/O and caught a parser
bug. See [awesome-machin](https://github.com/javimosch/awesome-machin).

- **Outbound networking — `dial(host, port)`.** Connect a TCP socket to a remote
  host (DNS-resolved via `getaddrinfo`), returning an fd used with the existing
  `read`/`write`/`close`. machin was server-only (`listen`/`accept`); `dial` makes
  it a client too — HTTP clients, health checkers, anything that reaches out.
  Surfaced and filled while building a real tool (the "build real things" goal).
- **`now_ms()` and `parse_int()`.** Wall-clock milliseconds (for measuring
  latency) and string→int parsing (`0` on non-numeric). Both surfaced building
  the same tool — a concurrent HTTP health checker.
- **File I/O — `read_file`, `write_file`, `list_dir`, `mkdir`.** Read/write whole
  files, list a directory (excludes `.`/`..`), make a directory. Native builtins
  (no FFI), surfaced building a static-site generator.
- **Parser fix — string literals equal to a structural token.** A string like
  `")"` was mistaken for the closing delimiter, so `index(s, ")")` failed to
  parse; value-list loops are now punctuation-aware. Caught by the SSG.
- **CLI builtins — `args()`, `env()`, `now()`.** `args()` returns the
  command-line arguments (`[]string`; `args()[0]` is the program path) — the
  generated `main` now takes `argc`/`argv`. `env(name)` reads an environment
  variable (`""` if unset). `now()` returns Unix seconds. Together these let MFL
  programs be real CLIs (subcommands, flags, `$PORT`, uptime) — the basis for a
  machin-based CLI/server boilerplate.

## v0.6.0

- **C FFI (Phases 1–3).** An `extern "lib" { header "..." link "..." cflags "..."
  cstruct T { f ctype ... } fn name(types) ret }` declaration names foreign C
  functions; calls compile to direct C calls and `header`/`link`/`cflags` are
  threaded into `cc`. **Phase 1:** scalar types — `int`/`float`/`bool`/`string`
  plus sized `i8…u64`/`f32`/`f64` (sizes matter for ABI: raylib takes 32-bit
  `int`/`float`). **Phase 2:** `cstruct` declares a C struct's layout; machin
  synthesizes a matching MFL struct and marshals it by value across the boundary
  (pass and return). **Phase 3:** the `ptr` type — an opaque C handle (`void*`,
  e.g. `FILE*` or a window/texture handle) held as an MFL `int` and passed back
  to C, never dereferenced. New `examples/complex/ffi_math.mfl`, `ffi_struct.mfl`,
  and `ffi_ptr.mfl`; the path to the C ecosystem and a native GUI.
- **Native GUI demo — `examples/gui/game_menu.mfl`.** A clickable Start / Settings
  / Exit menu drawn with [raylib](https://www.raylib.com) through the FFI: opens a
  real OpenGL window, draws rectangles/text with a `Color` cstruct, and polls the
  mouse each frame — proving Phases 1–2 are enough to drive a real graphics
  library. `extern` blocks may now have multiple `link` directives, kept in order
  (`-lraylib -lGL -lm -lpthread -ldl -lrt -lX11`). A GUI binary links the system
  graphics stack and needs a display — not a no-deps binary, as with any native GUI.
- **Tightened canonical form (token-minimization).** The canonical `.mfl` now
  drops whitespace adjacent to operators/punctuation (`fib(n - 1)` →
  `fib(n-1)`), keeping only the spaces the lexer needs between word tokens. Zero
  semantic change; ~13% fewer agent tokens to write/edit the corpus, measured
  with the new `tools/tokmin.py`. The same harness showed the *intuitive*
  minimizations are dead ends — `func`→`fn` saves **0** tokens (both are single
  tokens already) and `println`→`pln` is *worse* (abbreviations fragment) — so
  whitespace is where the win is.

## v0.5.0

- **Plain text is the source of truth.** The `.mfl` form is now canonical plain
  text — one normalized function per line — instead of base64. The reason is the
  language's own north star: measured with `tools/tokcost.py`, base64 costs an
  agent ~2.5× the output tokens to write/edit (and ~9× for a one-character edit),
  taxing the very machine-speed it was meant to signal. Text is greppable,
  diffable, and editable in place. `machin run` still reads the base64 form, now
  produced on demand by **`machin pack`** for distribution. Machine-first now
  means *shaped for machine authoring* (terse, inferred, canonical,
  function-addressable), not *encoded*.
- **`input()` builtin** — read a line from stdin (`() -> string`), enabling
  interactive / native desktop CLI programs. New `examples/complex/game_menu.mfl`.
- **`tools/tokcost.py`** — a tiktoken harness that measures the agent write/edit
  token cost of a source form; the instrument behind the plain-text decision.

## v0.4.2

- **Windows binaries.** Releases now also ship `machin-<tag>-windows-amd64.exe`,
  alongside linux/macOS × amd64/arm64. Five prebuilt binaries per release.

## v0.4.1

- **Release automation.** Pushing a `v*` tag now cross-compiles machin for
  linux/macOS × amd64/arm64 (pure Go, static, ~2 MB) and attaches the binaries
  plus `SHA256SUMS.txt` to the GitHub release — no manual upload step.

## v0.4.0

Native-language depth: safety, real closures, and bounded memory — plus the
platform layer (framework, router, `func` type) that landed since v0.3.0.

- **`--safe` build mode.** `machin run|build <file> --safe` inserts runtime
  checks: a slice index out of range, integer division/modulo by zero, or
  integer `+`/`-`/`*` overflow prints a Go-style `panic:` to stderr and exits
  non-zero. Opt-in — the default build keeps zero check overhead.
- **By-reference closure capture.** Closures now capture enclosing variables by
  reference (Go semantics): a captured variable lives in a shared cell, so a
  closure can mutate state that outlives the call that made it. The
  counter/accumulator idiom (`func counter() { n := 0  return func() { n = n + 1
  return n } }`) works, and sibling closures share one cell.
- **Scoped arenas (`arena { }`).** Wrapping a loop body in `arena { ... }`
  reclaims everything allocated inside the block when it ends, keeping a
  long-lived loop's memory flat (measured ~240 MB → ~1.4 MB over a 1M-iteration
  allocating loop). Blocks nest and compose with goroutines and `--safe`.

- **machweb — a web framework written in MFL.** `Request`/`Response` types,
  response builders (`ok_text`/`ok_html`/`ok_json`/`created`/`bad_request`/
  `not_found`), `parse_request`, a `param(path, prefix)` path helper, and
  `serve(port, handler)` which dispatches each request — in its own goroutine —
  to a handler closure `func(Request) Response`. A backend compiles to a single
  native binary with no runtime dependencies. See [`framework/`](framework/).
- **Map-based router.** `new_router()` → `route(r, method, path, handler)` →
  `serve_router(port, r)`. Handlers live in a `map[string]func` keyed by
  `"METHOD PATH"`; routing is method-aware and unmatched requests return `404`.
- **The `func` type.** A function-value type whose signature is inferred by
  unification — it lets closures be stored in slices, maps
  (`make(map[string]func)`), and struct fields. This is what makes a router's
  handler table possible.
- **Multi-file `machin encode`.** `encode` now accepts several source files and
  concatenates them, so a framework and an app compose into one program:
  `machin encode framework/machweb.src myapp.src > app.mfl`.

## v0.3.0

Ergonomics, toward feeling like Go to write:

- **Named return values.** `func divmod(a, b) (q, r) { q = a/b; r = a%b; return }`
  — the named returns are zero-initialized locals; a bare `return` (or falling
  off the end) yields them.
- **Variadic parameters.** A function's last parameter may be variadic
  (`func sum(nums...)`), collecting trailing call arguments into a slice. Call
  with extra args (`sum(1, 2, 3)`) or spread a slice (`sum(xs...)`). Variadics
  are generic — one source function specialized per element type.

## v0.2.1

- **Arena memory management.** Value buffers (strings, slice backings, closure
  environments) are allocated from a per-goroutine arena and reclaimed in bulk
  when the goroutine returns; the main goroutine's arena lives for the whole
  program. This bounds the memory of a long-running concurrent server — under a
  12,000-request load the self-host server's RSS plateaus at ~1.8 MB instead of
  growing unbounded. (Subsystems that free explicitly — channels, maps — keep
  raw allocation.)

## v0.2.0

A consolidation release. MFL grew from a base64 POC interpreter into a
native-compiling backend language with the complete Go-flavored core, plus a
formal specification ([`SPEC.md`](SPEC.md)).

### Language

- **Compilation to native code** — programs are translated to C99 and compiled
  with `cc -O2`; values are unboxed. `fib(40)` runs in ~0.20s, on par with
  hand-written C. (The original tree-walking interpreter was removed.)
- **Static typing by inference** — no annotations; type clashes are compile errors.
- **Composite types** — slices `[]T`, structs (`type T struct { ... }`), and
  maps `map[K]V` (int/string keys), all unboxed.
- **Control flow** — `for cond {}`, `for {}`, `while`, and `for k, v := range x`
  over slices, maps, and strings.
- **Multiple return values** — `return a, b`, destructuring `q, r := f()`,
  parallel assignment, and the comma-ok pattern.
- **Closures & first-class functions** — `func(x){...}` literals with by-value
  capture, higher-order functions (lambda-lifting + closure conversion).
- **Generics** — functions are implicitly generic, specialized per concrete
  call-site type by monomorphization (no boxing, no annotations).
- **Concurrency** — `go` goroutines (pthreads), channels (`make(chan T)`,
  `<-`), and `sleep`.
- **Networking & JSON** — BSD sockets (`listen`/`accept`/`read`/`write`/`close`),
  bidirectional JSON (`json(x)` serialize, `parse(s, T{})` parse), and string
  operations — enough to write a concurrent JSON-over-HTTP API with routing.

### Tooling

- `machin run` / `build` / `build --emit-c` / `encode`.
- `Makefile`, MIT `LICENSE`, `SPEC.md`, and 35 runnable examples.
- 51 Go tests exercising the full surface via the native path.

## v0.1.0

Initial POC: MFL as base64 (one function per line), a tree-walking interpreter,
`run`/`encode`/`decode`, and a first set of examples.
