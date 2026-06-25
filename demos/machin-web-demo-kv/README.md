# machin-web-demo-kv

In-memory key-value store REST API built with [machweb](../../framework/machweb.src).

**Exercises:** top-level `var` globals, `map[string]string`, machweb router pattern, goroutine-per-request concurrency.

## Build & run

```sh
./build.sh
./app          # → http://localhost:48080
```

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/v1/keys` | `{"keys":["a","b",...]}` |
| GET | `/v1/key/{k}` | Value string (200) or 404 |
| PUT | `/v1/key/{k}` | Set value from request body → `{"ok":true}` |
| DELETE | `/v1/key/{k}` | Remove key → `{"ok":true}` |

```sh
curl -X PUT  http://localhost:48080/v1/key/hello -d 'world'
curl         http://localhost:48080/v1/key/hello
curl -X DELETE http://localhost:48080/v1/key/hello
```
