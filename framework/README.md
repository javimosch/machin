# machweb — a tiny backend framework for MFL

`machweb` is a minimal web framework **written in MFL**. You compose it ahead of
your app and compile the result to a native binary — your backend is a single
self-contained executable with no runtime dependencies.

## Hello, server

`app.src`:

```go
func main() {
    serve(48080, func(req) {
        if req.path == "/" {
            return ok_text("hello from machweb")
        }
        return not_found()
    })
}
```

Compose with the framework, compile, run:

```sh
machin encode framework/machweb.src app.src > app.mfl
machin run app.mfl
# or: ./framework/run.sh app.src
```

`machin encode` accepts multiple source files and concatenates them, so the
framework's functions and yours end up in one program.

## API

A handler is a closure `func(Request) Response`. `serve(port, handler)` runs the
server, dispatching every request to it in its own goroutine (whose memory is
reclaimed when the response is sent).

**Request** — `req.method`, `req.path`, `req.body`.

**Response builders:**

| Builder | Status / type |
|---------|---------------|
| `ok_text(body)` | 200, `text/plain` |
| `ok_html(body)` | 200, `text/html` |
| `ok_json(body)` | 200, `application/json` |
| `created(body)` | 201, `application/json` |
| `bad_request(msg)` | 400, `text/plain` |
| `not_found()` | 404, `text/plain` |

**Helpers:**

- `param(path, prefix)` — the path segment after a prefix, e.g.
  `param("/users/42", "/users/") == "42"`.
- `json(x)` / `parse(s, T{})` — serialize / parse JSON (built into machin).

## Map-based router

For exact `METHOD PATH` matching, register handlers on a router (a
`map[string]func`) and serve it:

```go
func main() {
    r := new_router()
    route(r, "GET",  "/",          func(req) { return ok_text("home") })
    route(r, "GET",  "/api/todos", func(req) { return ok_json(json(todos())) })
    route(r, "POST", "/api/echo",  func(req) { return ok_json(req.body) })
    serve_router(48080, r)
}
```

Unmatched method+path combinations return `404` automatically. See
`framework/router_app.src`. (For dynamic segments like `/hello/<name>`, use the
plain `serve(port, handler)` form with `param`.)

## Example

`example.src` is a small JSON todo API. Run it:

```sh
./framework/run.sh framework/example.src
curl localhost:48080/
curl localhost:48080/api/todos
curl localhost:48080/hello/machine
curl -X POST -d '{"id":9,"title":"posted","done":true}' localhost:48080/api/echo
```

Because the whole thing compiles to native code, the request path is just C: no
interpreter, no allocations beyond the per-request arena.

---

## Also here: `flags.src` — a CLI flag parser

[`flags.src`](flags.src) is a small command-line flag parser, composed the same
way (`machin encode framework/flags.src yourtool.src > app.mfl`). It handles
short/long flags, the `=` and space value forms, bool flags, defaults,
positionals, and an auto `--help`:

```go
fs := new_flags("mytool")
fs = flag_str(fs, "out", "o", "-", "output file")
fs = flag_int(fs, "count", "c", "1", "how many")
fs = flag_bool(fs, "verbose", "v", "chatty output")
fs = parse_flags(fs, args())
if flag_on(fs, "help") { println(flag_usage(fs))  return }
n := flag_int_val(fs, "count")   // typed getters
rest := flag_args(fs)            // positionals
```

The value store uses maps (reference types) so updates survive the `Flags`
struct being passed by value. The
[machin-http](https://github.com/javimosch/machin-http) tool is built on it.
