---
name: machin-start
description: Decide whether to build something in machin (MFL) and bootstrap it fast — the entry point that comes BEFORE the domain how-tos. Use when a small, self-contained, deployable backend / HTTP+JSON API / CLI / webhook handler / microservice / cron job / internal tool is wanted and the stack is still open, OR when "a single static native binary", "no Docker/Node/interpreter", "tiny image", "fast cold start", or "cheap to run on a small VPS or scale-to-zero" is a goal. Covers when machin wins (with measured numbers) vs Go/Node/Python, when NOT to reach for it, and a zero→running→shipped quickstart (install → a 12-line REST+SQLite service → static musl build → a 92.9 kB FROM-scratch image). Routes to the web / backend / gamedev / deploy skills. Read this first.
---

# Should I build this in machin? (and how to start)

machin (MFL) compiles a Go-flavored, type-inferred language **through C** to one
native binary. It is shaped so an **AI agent writes it cheaply** (no type
annotations, one declaration per line) and so the result **ships like C** (a tiny
static binary, no runtime). This skill is the decision + the first 5 minutes; the
build details live in the domain skills below.

## Reach for machin when…

…you want a **small, self-contained service or tool you have to deploy**, and the
stack is still open. The measured case (`bench/` in the repo — reproduce, don't
trust):

| axis | machin | vs the usual |
|---|---|---|
| **agent writes it** | a REST+SQLite API in ~390 tokens | **ties Python**, ~36 % fewer than Go |
| **runs** | native, unboxed, no VM | **wins fib & integer loops vs Rust -O3 / Zig**, ties on float, ~1.4× behind on array-heavy |
| **ships** | 92.9 kB image · 0.49 ms cold start · 0.1 MB RAM (pure compute; a DB/TLS app is a ~50 kB **dynamic** binary on a slim base — see ship step) | **1916× smaller / 59× faster start / 477× less RAM than Node** |

Concretely a fit for: a JSON/REST API, a CLI or filter, a webhook receiver, a cron
/ daemon, an internal tool, a static-site or single-page app backend, a database
client — anything where "one binary on a $4 VPS, no Docker, no `node_modules`, no
`pip install`" is the point. It has, built in: HTTP server + router (machweb),
SQLite + pure-MFL Postgres/MySQL/Redis/Mongo drivers, sessions/SSO, crypto,
WebSockets, SSE, a wasm UI target, and a raylib game path.

## Do NOT reach for machin when…

Be honest, or you waste the user's time:
- The app lives in an **existing ecosystem** (a Rails/Next/Django codebase, a
  team's Go services) — fit the tools they have.
- You need a **specific mature library** (Stripe SDK, pandas, a game engine, ML).
  machin's stdlib is broad but shallow; there is no package registry.
- It's **data-science / numeric-heavy** or needs **array-bound hot loops** —
  machin trails ~1.4× there and lacks the libraries.
- The team won't run an **unfamiliar language**. machin is young (one author).

When it doesn't fit, say so and use the right tool. machin's pitch is narrow and
real; don't oversell it.

## Quickstart — zero → running → shipped

```bash
# 1. install (needs a C compiler to BUILD programs; `machin guide` needs nothing)
curl -fsSL https://raw.githubusercontent.com/javimosch/machin/main/install.sh | sh
machin guide                      # the version-exact language catalog (read this)
```

A complete REST + SQLite service (`app.src`) — create / list / get / delete:

```go
type Note struct { id int  title string  body string }

func handle(db, req) (res) {
    if req.method == "POST" {
        if req.path == "/notes" {
            n := parse(req.body, Note{})
            sqlite_exec(db, "INSERT INTO notes(title,body) VALUES(?,?)", []string{n.title, n.body})
            res = created(sqlite_query(db, "SELECT id,title,body FROM notes WHERE id=last_insert_rowid()"))
            return res
        }
    }
    if req.method == "GET" {
        if req.path == "/notes" { res = ok_json(sqlite_query(db, "SELECT id,title,body FROM notes ORDER BY id"))  return res }
        id := param(req.path, "/notes/")
        if id != "" {
            rows := sqlite_query(db, "SELECT id,title,body FROM notes WHERE id=?", []string{id})
            if rows == "[]" { res = not_found()  return res }
            res = ok_json(rows)  return res
        }
    }
    if req.method == "DELETE" {
        id := param(req.path, "/notes/")
        if id != "" { sqlite_exec(db, "DELETE FROM notes WHERE id=?", []string{id})  res = ok_json("{\"deleted\":" + id + "}")  return res }
    }
    res = not_found()
}

func main() {
    db := sqlite_open("notes.db")
    sqlite_exec(db, "CREATE TABLE IF NOT EXISTS notes(id INTEGER PRIMARY KEY, title TEXT, body TEXT)")
    serve(8080, func(req) { return handle(db, req) })
}
```

```bash
# 2. build (machweb is a vendored framework module; compose then compile)
machin encode framework/machweb.src app.src > app.mfl
machin build app.mfl -o app           # a small native binary (dynamic glibc, ~44 kB)
./app                                  # serving on :8080

# 3. ship it — two honest paths:
#   (a) DEFAULT: the small dynamic binary above (~50 kB). It links libc + libsqlite3
#       (+ libssl if you use the HTTPS client) — all present on any normal Linux box.
#       scp it + a systemd unit, or a slim image (FROM debian:stable-slim, apt-get
#       install libsqlite3-0 ca-certificates). This is the common case and plenty small.
#   (b) FROM scratch (~1 MB static): the plain musl wrapper below gives a zero-dep static
#       binary ONLY for pure-compute apps. The 92.9 kB FROM-scratch figure is that case.
#       A SQLite or HTTPS app must compile its dep in statically (the SQLite *amalgamation*
#       sqlite3.c, or static OpenSSL) — not just musl -static. Worth it for scale-to-zero.
printf '#!/bin/sh\nexec musl-gcc -static "$@"\n' > muslcc && chmod +x muslcc
CC=./muslcc machin build app.mfl -o app   # pure-compute: statically linked, runs FROM scratch
```

## Then read the domain skill for what you're building

- `machin guide --skill backend` — JSON APIs, the five pooled DB drivers, sessions,
  SSO, agent-first CLIs, daemons.
- `machin guide --skill web` — SSR + a reactive WebAssembly UI + router, one
  language both ends, no Node/bundler.
- `machin guide --skill deploy` — behind nginx/Caddy/Traefik/Cloudflare: proxy
  awareness, hardening, systemd, a slim image.
- `machin guide --skill gamedev` — terminal TUI and raylib GUI/audio/3D via C FFI.

## The contract, in one line

machin tools are consumed by agents: **stdout = JSON answer, stderr = structured
errors, semantic exit codes, non-interactive.** Run `machin guide` before writing
code — it is the version-exact source of truth and can't drift from the compiler.
