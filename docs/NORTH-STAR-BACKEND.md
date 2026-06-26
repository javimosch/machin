# North star — machin as an SME backend

The bet: a small/medium business's backend can be **one static machin binary** — CLI
+ HTTP server + JSON API + datastore + auth — with no runtime, no `node_modules`, no
container sprawl. The web domain (`docs/NORTH-STAR-WEB.md`) proved the HTTP/SSR/wasm
side. This file tracks the **service-plumbing** side: datastores, auth, and the
integrations a real SME app needs.

Same rule as every machin domain: **grown by building real things.** Each integration
is a dogfood — we build the smallest real client/library, and the gaps it hits get
filled in the language (not worked around). The PostgreSQL client (v0.60.0) already
paid for two: `read_bytes` and binary-safe base64.

## Capability matrix

| Capability | Status | Notes |
|---|---|---|
| Embedded SQL | ✅ | SQLite builtins (`sqlite_*`), `parse(rows, []T{})` decode |
| **Networked SQL — Postgres** | ✅ | `framework/postgres.src`: wire v3 + SCRAM-SHA-256; `pg_query` (simple) + `pg_exec` (**parameterized**, `$1`/extended protocol, injection-safe) → JSON rows. **Gap left:** connection reuse/pool, COPY, TLS |
| Networked SQL — MySQL | ❌ | needs SHA1 (mysql_native_password) or the caching_sha2 flow |
| Redis | ✅ | `framework/redis.src`: RESP over `dial()`; typed helpers + `redis_cmd`; arrays → JSON → `parse([]string{})`. Cache/sessions/counters/queues |
| MySQL / MariaDB | ✅ | `framework/mysql.src`: wire protocol + mysql_native_password (SHA-1) auth; typed JSON rows → `parse([]T{})`. **Gap left:** caching_sha2 auth, prepared statements, pooling |
| MongoDB | ✅ | `framework/mongo.src` + `bson.src`: OP_MSG + BSON; insert/find/count/drop, docs → JSON → `parse([]T{})`. **SCRAM-SHA-256 auth**, doubles, cursor pagination, query/delete by `_id`, **connection pooling** |
| HTTP server | ✅ | `framework/machweb.src` (router, body, response builders, binary path) |
| HTTP/TLS client | ✅ | `https_get`/`https_post`/`http_request`; `wss_*` WebSocket |
| Crypto | ✅ | sha256, hmac, hkdf, ed25519, x25519, aes-gcm/cbc, rand_bytes; bitwise ops |
| Sessions / cookies | ✅ | `cookie(req,name)` + `set_cookie`/`clear_cookie`; signed sessions `set_session`/`get_session` (HMAC tag, unforgeable) |
| SSO / OAuth2 / OIDC | ✅ | `framework/sso.src`: authorization-code login on machweb sessions; identity via the userinfo endpoint (no JWT/RSA). **Gap left:** local id_token (RS256) verification |
| Email / SMTP | ❌ | SMTP over `dial()`/TLS |
| Background jobs / cron | ❌ | a scheduler loop + goroutines; persistence via the DB |
| Config / secrets | 🟡 | `env()` exists; a `.env` loader + typed config would help |
| Structured logging | ❌ | a small JSON-line logger |
| Migrations | ❌ | versioned schema apply (SQLite + Postgres) |

✅ shipped · 🟡 partial · ❌ gap

## Roadmap (dogfood order)

1. **The rest as pulled by real apps** — SMTP, a job scheduler, a `.env`/config loader,
   migrations, a JSON logger, plus Postgres COPY/TLS. Build when a dogfood app needs them.

## Milestones

| Built | What it added / surfaced |
|---|---|
| **PostgreSQL client** ([`framework/postgres.src`](../framework/postgres.src), v0.60.0) | First networked datastore: wire v3 + SCRAM-SHA-256, simple query → JSON rows that `parse([]T{})` decodes. Surfaced + filled `read_bytes` (NUL-safe socket read) and `base64_*_bytes` (binary base64). Pure MFL, no cgo. |
| **Postgres parameterized queries** (`pg_exec`, v0.61.0) | Extended query protocol (Parse/Bind/Describe/Execute/Sync): `$1`/`$2` params bound server-side, injection-safe; SELECT + INSERT/UPDATE/DELETE. Closes the top v0.60.0 follow-up. |
| **Cookies + signed sessions** (machweb, v0.62.0) | `cookie`/`set_cookie`/`clear_cookie` + unforgeable HMAC sessions (`set_session`/`get_session`). The auth foundation — the half of login that isn't the identity provider. |
| **SSO — OAuth2/OIDC** (`framework/sso.src`, v0.63.0) | "Log in with Google/Microsoft" on top of the sessions: `sso_begin`/`sso_complete`, identity via userinfo (no JWT/RSA). Surfaced + fixed a compiler bug — an omitted string struct field was NULL (now `""`). Added machweb `redirect`/`query`. |
| **Redis client** (`framework/redis.src`, v0.64.0) | RESP over `dial()`, no client lib; typed helpers + `redis_cmd`; arrays → JSON → `parse([]string{})`. Cache, sessions, counters, queues. Same pure-MFL-client pattern as Postgres. |
| **Connection pooling** (Postgres + Redis v0.65.0, **Mongo v0.71.0**) | Handle-based connections + an async-channel pool (`pg_pool_init`/`pg_acquire`/`pg_release`, `redis_pool_init`/…). Makes the datastore clients concurrency-safe under machweb's per-request goroutines — the gap the SaaS demo surfaced. No new language feature: machin channels are already unbounded queues (a semaphore). |

The language is now a credible single-binary SME backend: HTTP server (cookies, signed
sessions, **SSO login**) + SSR/wasm UI + **SQLite / Postgres / Redis** (safe parameterized
queries, cache/queues, **pooled for concurrency**) + a rich crypto kit. Auth is end-to-end,
the core datastores are covered, and a [worked SaaS demo](https://github.com/javimosch/machin-saas-demo)
ties it together under concurrent load. What's left is breadth — SMTP / jobs / config /
migrations / logging, built as real apps pull them in.
