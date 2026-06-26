---
name: machin-backend
description: Build a single-binary backend service in machin (MFL) — HTTP/JSON APIs, five pooled datastores (SQLite, PostgreSQL, MySQL/MariaDB, Redis, MongoDB), signed sessions, OAuth2/OIDC SSO, and agent-first CLIs — all pure MFL, one static binary, no Node/ORM/cgo. Use when writing or debugging a machin backend: a REST/JSON service, a database client or migration, an auth flow, a daemon, or a headless-CMS-style tool. Covers the machweb/postgres/mysql/redis/mongo/bson/sso frameworks, the uniform JSON-rows + parse() pattern, connection pooling for concurrency, the agent-first CLI contract, and the hard-won gotchas. Distilled from the backend dogfood (machin v0.60–v0.74) and the MachNotes / machin-db-migrate / machin-cms apps.
---

# Building backends in machin

machin compiles MFL to one **static native binary** — an HTTP server, a database client,
auth, and a CLI in the same program, no Node, no ORM, no cgo, no client libraries. The
datastore drivers are **pure MFL** over `dial()` + the crypto builtins.

> Run `machin guide` first (the version-exact language catalog). For the web/UI side —
> SSR, the reactive wasm UI, the router — read `machin guide --skill web`. This skill is
> the *server / data / auth* how-to.

## The HTTP server — `framework/machweb.src`

A handler is `func(Request) Response`; `serve(port, handler)` runs it, **one goroutine
per connection** (so shared state must be pooled — see below). `req.method` / `req.path`
(carries the query string) / `req.body` / `header(req,name)` / `cookie(req,name)` /
`query(req,name)`. Builders: `ok_text` `ok_html` `ok_json` `ok_bytes(ctype,b)` `ok_wasm` ·
`created` `bad_request` `not_found` · `redirect(url)`. A map router: `new_router()`,
`route(r,"GET","/x",h)`, `serve_router(port,r)`.

```go
func handle(req) (res) {
    if has_prefix(req.path, "/api/users") { return ok_json(users_json()) }
    res = not_found()
}
func main() { serve(8080, func(req) { return handle(req) }) }
```

## Datastores — one uniform shape

**Every driver returns rows as a JSON-array-of-rows STRING, so `parse(rows, []T{})`
decodes them into a typed slice** (numeric/bool columns come back unquoted). That single
idiom works across all five. `json_get(rows, "[0].field")` pulls one value (but returns
the *raw* token — a string stays quoted; prefer `parse`).

| Store | Connect | Query / exec |
|---|---|---|
| **SQLite** (embedded) | `db := sqlite_open(path)` / `:memory:` | `sqlite_query(db, sql[, []string params])` → JSON · `sqlite_exec(db, sql[, params])` (`?`-bind) · `sqlite_close` |
| **PostgreSQL** (`postgres.src`) | `pg_connect(host,port,user,db,pass)` + SCRAM | `pg_query(sql)` (trusted) · `pg_exec(sql, []string params)` (`$1`, extended protocol, injection-safe) · `pg_disconnect` |
| **MySQL/MariaDB** (`mysql.src`) | `mysql_connect(host,port,user,pass,db)` (native_password) | `mysql_query(sql)` → JSON · `mysql_exec(sql)` → affected · `mysql_escape(s)` · `mysql_close` |
| **Redis** (`redis.src`) | `redis_connect(host,port)` [+ `redis_auth(pw)`] | `redis_set/setex(k,secs,v)/get(k)->(v,ok)/del/incr/expire/rpush/lpush/lrange/keys` · `redis_cmd([]string)` |
| **MongoDB** (`mongo.src`+`bson.src`) | `mongo_connect(host,port)` [+ `mongo_auth("admin",user,pw)`] | `mongo_insert_one(db,coll,doc)` · `mongo_find_all/find(db,coll[,filter])` · `mongo_find_by_id` · `mongo_count/drop/delete` · `mongo_close` |

```go
type User struct { id int  name string  email string }
pg_connect("127.0.0.1", 5432, "postgres", "app", "secret")
rows  := pg_exec("SELECT id, name, email FROM users WHERE active = $1", []string{"1"})
users := parse(rows, []User{})          // typed slice, values decoded
```

**Mongo documents are BSON** — build with the `bson.src` builder, decode replies via the
driver (which renders to JSON): `bson_new()` then `bson_str`/`bson_i32`/`bson_i64`/
`bson_double`/`bson_bool`/`bson_null`/`bson_oid(key,hex)`/`bson_subdoc`/`bson_subarr`,
finalize with `bson_finish`. `_id` ObjectIds decode to a hex string; query by it with
`mongo_find_by_id`. SCRAM-SHA-256 auth, doubles, and cursor pagination (`getMore`) are all
handled.

## Connection pooling (a concurrent server)

machweb runs each request in its own goroutine, so a **shared single connection would
interleave**. Every networked driver is **handle-based + poolable** — the pool is an
async channel of authenticated connections (a semaphore, built on machin's unbounded
channels; no new primitive needed):

```go
pg_pool_init(8, "127.0.0.1", 5432, "postgres", "app", "secret")   // once, in main
func handle(req) (res) {
    c := pg_acquire()                       // per request -> its own connection
    rows := pgx(c, "SELECT ... WHERE id = $1", []string{id})
    pg_release(c)
    res = ok_json(rows)
}
```

Handle ops per driver (the global `*_connect`/`*_query` API stays for single-connection
scripts): Postgres `pg_acquire`/`pgq`/`pgx`/`pg_release`; MySQL `mysql_acquire`/`myq`/
`myx`/`mysql_release`; Redis `redis_acquire`/`rset`/`rget`/…/`redis_release`; Mongo
`mongo_acquire`/`mins`/`mfind`/`mfindall`/`mfindid`/`mdel`/`mcount`/`mdrop`/`mongo_release`.
SQLite "pools" as several `sqlite_open` handles (use WAL + `PRAGMA busy_timeout`).

## Auth — sessions + SSO (`framework/machweb.src`, `sso.src`)

- **Signed sessions** (a cookie the client can't forge): `set_session(res, secret, "sid",
  userID)` stores `userID` + an HMAC-SHA256 tag; `get_session(req, secret, "sid") ->
  (value, ok)` returns `ok==1` only if it verifies. `cookie(req,name)`, `set_cookie` /
  `clear_cookie` (safe defaults `Path=/; HttpOnly; SameSite=Lax`). Keep `secret`
  server-side (`env`); the value is signed, not encrypted — store an id, not secrets.
- **SSO ("log in with Google/Microsoft/…")**: fill `OAuthProvider{auth_url, token_url,
  userinfo_url, client_id, client_secret, redirect_uri, scope}`; route `GET /login` →
  `sso_begin(p, secret)` (302 + signed CSRF state), `GET /callback` → `profile, ok :=
  sso_complete(p, secret, req)` (verify state, exchange code, fetch userinfo). Identity
  comes from the userinfo endpoint, so no JWT/RSA is needed. Then `set_session(...)`.
- A common pattern is **sessions in Redis** (`rsetex("sess:"+sid, 3600, email)`) keyed by
  a signed cookie — see the MachNotes app.

## Crypto you'll reach for

`sha256` / `hmac_sha256` (hex), `sha256_bytes` / `hmac_sha256_bytes` / `sha1_bytes`
(binary), `rand_bytes(n)`, `base64_encode/decode` + `_bytes` variants, `hkdf_sha256`,
`aes_gcm_encrypt/decrypt`, `ed25519_*` / `x25519_*`. `to_hex(rand_bytes(16))` is the
idiomatic token/id.

## CLIs — be agent-first

A backend tool is consumed by agents. Follow the contract (see
[`AGENTS_FRIENDLY_TOOLS.md`] in your project, and the machin-cms app):
- **stdout = the answer as JSON** (`{"ok":true,"data":...}`); **stderr = structured
  errors** (`write(2, json+"\n")` — fd 2 is stderr) so logs never pollute the data stream.
- **Semantic exit codes**: `0` ok · `80–89` input/validation · `90–99` resource
  (not found / exists) · `100–109` integration (db/auth down) · `110–119` internal.
  `exit(code)`.
- **Non-interactive**, deterministic, composable subcommands; a `help-json` for
  introspection. Parse args from `args()` (`args()[0]` is the program path).

## Daemons (start/stop without PIDs)

`system(cmd) -> int` runs a shell command (`-1` if unlaunchable). `daemon start` spawns a
detached server and returns: `system(args()[0] + " serve " + port + " >log 2>&1 &")`;
`stop` POSTs an internal `/_shutdown` route (the handler calls `exit(0)`); `status` probes
`/_health`. No pidfiles or signals — see machin-cms.

## Build & verify

- Compose vendored frameworks + your app, then build: `machin encode framework/machweb.src
  framework/postgres.src app.src > app.mfl && machin build app.mfl -o app`. The drivers
  link only what they use (`-lsqlite3` for SQLite, OpenSSL for crypto/SCRAM, `-lm`, …).
- **`machin encode` runs the typechecker** — most errors surface there (no `cc` needed).
- **Verify against a real server** in a container (`docker run -d -p 5432:5432 postgres:16`,
  `redis:7`, `mongo:7`, `mariadb:11`) and drive your binary with `curl`. The machin repo's
  gated tests (`MACHIN_PG_TEST=1`, `MACHIN_MYSQL_TEST=1`, `MACHIN_MONGO_TEST=1`, …) show the
  pattern; pool concurrency is proven with N goroutines over K connections.

## Gotchas (hard-won)

- **Pool, don't share.** A single global connection in a concurrent server corrupts reads.
  Use the pool + acquire/release per request, releasing on **every** return path.
- **Wire reads are NUL-binary** — the drivers use `read_bytes` (not `read`, which is a
  C string and truncates at a NUL) and binary-safe `base64_*_bytes` (for SCRAM).
- **`json_get` returns the raw token** — a string field comes back quoted (`"Ada"`).
  Prefer `parse(rows, []T{})`; strip quotes only when pulling one field.
- **Parameterize** (`?` / `$1` / Mongo filter docs) for user input; `mysql_escape` for the
  text protocol. Never string-concat user input into SQL.
- **A function named like a builtin is a compile error** (`len`/`str`/`keys`/…); and two
  structurally-identical structs (e.g. two `{fd int  buf []bytes}` driver handles) can
  confuse the checker — give the same-scope variable holding each a distinct name.

## Worked apps + direction

- **[machin-saas-demo](https://github.com/javimosch/machin-saas-demo)** (MachNotes) — SSO →
  Redis sessions → pooled Postgres → reactive wasm UI, one binary.
- **[machin-cms](https://github.com/javimosch/machin-cms)** — an agent-first, db-agnostic
  headless CMS (schemas, RBAC, a REST API daemon over SQLite/MySQL/Mongo, all pooled).
- **[machin-db-migrate](https://github.com/javimosch/machin-db-migrate)** — SQLite ⇄ MongoDB,
  both directions.
- Direction + capability/gap matrix: [`docs/NORTH-STAR-BACKEND.md`](../../docs/NORTH-STAR-BACKEND.md).
