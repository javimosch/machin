---
name: machin-deploy
description: Ship a machin (MFL) web app to production — run it correctly and safely behind a reverse proxy (nginx / Caddy / Traefik / Cloudflare), with the machweb hardening + proxy-awareness knobs, a systemd unit, and a slim Docker image. Use when deploying or operationalizing a machin HTTP service: getting HTTPS via a proxy, fixing http→https redirects/cookies behind TLS termination, the real client IP, request size/time limits, access logs, and the run/restart story. Distilled from the deploy dogfood (machin v0.78) and the machin-deploy reference app.
---

# Deploying a machin web app

A machin web service is **one static-ish native binary** (it links libc, OpenSSL, and
maybe libsqlite3 — not fully static, but no runtime/interpreter). The standard production
shape is that binary **behind a reverse proxy** that terminates TLS — nginx, Caddy,
Traefik, or Cloudflare. You do **not** need machin to speak TLS itself; the proxy does
HTTPS and forwards plain HTTP to your app. This skill is about making the app *correct and
safe* in that setup.

`serve_tls(port, certfile, keyfile, handler)` (`framework/machweb.src`) lets machweb
terminate HTTPS itself with no proxy in front — for a simple/internal service where you
already have a cert+key file and don't want another moving part. It's **not yet a
replacement for the proxy setup below** for anything public-facing: no ACME/Let's Encrypt
auto-renewal (bring your own cert, renew it yourself), and `res.is_hijack`/`res.is_stream`
(protocol upgrades, SSE) aren't supported over it yet — use `serve` behind a proxy for
those. Reach for `serve_tls` when you want zero infra for a small/internal HTTPS service;
reach for the reverse-proxy shape below for anything public production-facing.

> Build the app with the [`machin-web`](web) / [`machin-backend`](backend) skills; this is
> the *operate it in production* how-to.

## Turn on the production knobs (in `main`)

All default **off**, so dev is unchanged. Enable what you need before `serve`:

```go
func main() {
    harden(20 * 1024 * 1024, 15000)   // body cap 20 MB, 15 s read timeout, +trust-proxy,
                                       // +Secure cookies, +access log — the common set
    serve(8080, func(req) { return handle(req) })
}
```

`harden(max_body_bytes, read_timeout_ms)` is shorthand; the individual switches are:

| Call | Effect |
|---|---|
| `set_trust_proxy(1)` | trust `X-Forwarded-Proto` / `X-Forwarded-For` (**only** behind a proxy you control) |
| `set_secure_cookies(1)` | add `; Secure` to every cookie (you're served over HTTPS) |
| `set_max_body(n)` | reject a request body larger than `n` bytes with `413` — *without buffering it* |
| `set_read_timeout(ms)` | cap a slow client's request read (anti **slow-loris**) |
| `set_access_log(1)` | one JSON access-log line per request on **stderr** (fd 2) |

## Be proxy-correct

Behind a TLS-terminating proxy the socket is plain HTTP, so without help your app thinks
every request is `http://` and sees the *proxy's* IP. With `set_trust_proxy(1)`:

- **`scheme(req)`** → `http`/`https` from `X-Forwarded-Proto`. Use it so redirects, OAuth
  `redirect_uri`s, and emailed links are `https`, not `http`.
- **`base_url(req)`** → `scheme://host` — build absolute URLs that survive the proxy.
- **`client_ip(req)`** → the real client (left-most `X-Forwarded-For` hop), for logging /
  rate-limiting / audit. `req.remote` is the raw socket peer (the proxy) if you need it.
- **`set_secure_cookies(1)`** → sessions get `Secure` so they're never sent in clear.

**Only trust these headers behind a proxy that sets them** — a client can forge
`X-Forwarded-*`. If the app is ever directly exposed, leave `set_trust_proxy(0)`.

## Reverse proxy snippets

**nginx** — forward the headers machweb reads, and don't buffer SSE:
```nginx
location / {
    proxy_pass         http://127.0.0.1:8080;
    proxy_set_header   Host $host;
    proxy_set_header   X-Forwarded-Proto $scheme;
    proxy_set_header   X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_buffering    off;          # let Server-Sent Events stream (machweb also sends X-Accel-Buffering: no)
}
```
**Caddy** — TLS + the forwarded headers are automatic:
```
app.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

## Run it: systemd

A single binary is a trivial unit. `Type=simple`, run as a non-root user, restart on
failure, and rely on `set_access_log(1)` → journald:
```ini
[Unit]
Description=machin app
After=network.target

[Service]
ExecStart=/opt/app/server
Environment=PORT=8080 APP_SECRET=change-me
User=app
Restart=on-failure
RestartSec=2
# SIGTERM stops it; in-flight requests are short-lived goroutines.

[Install]
WantedBy=multi-user.target
```
`listen` sets `SO_REUSEADDR`, so a restart rebinds immediately (no `TIME_WAIT` wait).

## Run it: Docker

By default the binary needs libc + OpenSSL (`libssl`/`libcrypto`, for sessions/crypto) and
`libsqlite3` (if you use SQLite) at runtime, so ship it on a **slim base**:
```dockerfile
FROM debian:stable-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      libssl3 libsqlite3-0 ca-certificates && rm -rf /var/lib/apt/lists/*
COPY server /usr/local/bin/server
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]
```
(Multi-stage: build with `machin build` in a builder stage, copy just the binary.)

**`FROM scratch` for a SQLite-only service:** `machin build --static` compiles the SQLite
amalgamation in (no `libsqlite3`); with `CC=musl-gcc` you get a libc-free ~1 MB binary that
runs from an empty image — `FROM scratch / COPY server / ENTRYPOINT`. This works when the
program uses **no OpenSSL** — i.e. no HTTPS client (`https_*`) and no crypto builtins
(`hmac_sha256`/`sha256`/`rand_bytes`/…, which signed sessions use). Those still link OpenSSL
(so the slim base above) until native TLS lands (issue #260).

## Gotchas

- **Trust the proxy, not the client.** `set_trust_proxy(1)` only behind a proxy that
  *overwrites* `X-Forwarded-*`. Directly exposed → keep it off, or a client spoofs its IP.
- **`set_secure_cookies(1)` requires HTTPS.** On a Secure cookie set over plain `http://`
  the browser drops it — turn it on only once TLS (via the proxy) is in front.
- **Access logs go to stderr** so stdout stays a clean data/JSON stream; capture fd 2 in
  journald/Docker.
- **Body cap is on the declared `Content-Length`** — an over-cap request is `413`'d before
  its body is read (no OOM, no draining megabytes).
- **Graceful shutdown is SIGTERM = exit.** Each request is its own short-lived goroutine,
  so kill is safe in practice; there's no long-drain coordinator yet.

## Reference

- **[machin-deploy](https://github.com/javimosch/machin-deploy)** — a production-ready
  machin service wired with all of the above: proxy-correct, hardened, a systemd unit, a
  slim Dockerfile, and nginx/Caddy configs. Clone it as a deploy template.
