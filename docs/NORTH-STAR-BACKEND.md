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
| Redis | ❌ | RESP is a simple text protocol over `dial()` — small build; cache/sessions/queues/rate-limit |
| MongoDB | ❌ | lower priority — Postgres + JSON covers most document needs |
| HTTP server | ✅ | `framework/machweb.src` (router, body, response builders, binary path) |
| HTTP/TLS client | ✅ | `https_get`/`https_post`/`http_request`; `wss_*` WebSocket |
| Crypto | ✅ | sha256, hmac, hkdf, ed25519, x25519, aes-gcm/cbc, rand_bytes; bitwise ops |
| Sessions / cookies | ❌ | machweb parses headers but has no `Set-Cookie`/cookie helpers |
| SSO / OAuth2 / OIDC | ❌ | buildable on the HTTP client + crypto (HS256 = hmac_sha256); RS256 needs RSA (gap) |
| Email / SMTP | ❌ | SMTP over `dial()`/TLS |
| Background jobs / cron | ❌ | a scheduler loop + goroutines; persistence via the DB |
| Config / secrets | 🟡 | `env()` exists; a `.env` loader + typed config would help |
| Structured logging | ❌ | a small JSON-line logger |
| Migrations | ❌ | versioned schema apply (SQLite + Postgres) |

✅ shipped · 🟡 partial · ❌ gap

## Roadmap (dogfood order)

1. **Sessions/cookies in machweb.** `Set-Cookie` + cookie parsing + a signed-cookie
   session helper (HMAC over `hmac_sha256_bytes`). Foundational for anything with login.
2. **SSO library.** OAuth2 authorization-code login (Google/Microsoft) on top of (1):
   `https_post` token exchange + `https_get` userinfo + `rand_bytes` state. Surfaces the
   RSA/RS256 gap (sidestep via the userinfo endpoint for v1).
3. **Redis client.** RESP over `dial()` — cache, sessions, rate-limit, simple queues.
   Proves the same "pure-MFL client" pattern Postgres established; small and high-value.
4. **Connection pooling / reuse** for the Postgres client (keep a connection open
   across requests instead of dial-per-query), plus COPY and TLS when an app needs them.
5. **The rest as pulled by real apps** — SMTP, a job scheduler, a `.env`/config loader,
   migrations, a JSON logger. Build when a dogfood app needs them.

## Milestones

| Built | What it added / surfaced |
|---|---|
| **PostgreSQL client** ([`framework/postgres.src`](../framework/postgres.src), v0.60.0) | First networked datastore: wire v3 + SCRAM-SHA-256, simple query → JSON rows that `parse([]T{})` decodes. Surfaced + filled `read_bytes` (NUL-safe socket read) and `base64_*_bytes` (binary base64). Pure MFL, no cgo. |
| **Postgres parameterized queries** (`pg_exec`, v0.61.0) | Extended query protocol (Parse/Bind/Describe/Execute/Sync): `$1`/`$2` params bound server-side, injection-safe; SELECT + INSERT/UPDATE/DELETE. Closes the top v0.60.0 follow-up. |

The language is now close to a single-binary SME backend: HTTP server + SSR/wasm UI +
**SQLite or Postgres** (with safe parameterized queries) + a rich crypto kit. The main
gap left is **auth** — sessions/cookies, then SSO — next on the roadmap.
