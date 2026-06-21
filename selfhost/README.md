# selfhost — the project, served by MFL

`server.mfl` is a component of this project **written in MFL itself** and
compiled to a native binary by `machin`. It serves the project's info and
example catalog over HTTP — dogfooding most of the language in one program:

- **sockets + concurrency** — a goroutine per connection (`go handle(conn)`)
- **string ops** — parses the request line and routes by path
- **structs + slices + `range`** — builds the example catalog and the HTML page
- **JSON** — serializes structs (including a struct with a `[]string` field and a
  slice of structs) for the API

## Run

```sh
machin run selfhost/server.mfl
# or build a binary:
machin build selfhost/server.mfl -o selfhost-bin && ./selfhost-bin
```

Then:

| Route | Returns |
|-------|---------|
| `GET /` | an HTML index built from the example catalog |
| `GET /api/info` | JSON: name, version, tagline, features |
| `GET /api/examples` | JSON array of `{name, category, blurb}` |
| anything else | `404` |

```sh
curl  http://localhost:48080/
curl  http://localhost:48080/api/info
curl  http://localhost:48080/api/examples
```
