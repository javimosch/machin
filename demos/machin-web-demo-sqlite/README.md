# machin-web-demo-sqlite

Persistent notes app with SQLite storage, built with [machweb](../../framework/machweb.src).

**Exercises:** `sqlite_open`, `sqlite_exec`, `sqlite_query`, parameterized queries (injection-safe `[]string` params), JSON responses from SQLite directly.

## Build & run

```sh
./build.sh
./app          # → http://localhost:48080
```

Notes are persisted in `notes.db` (SQLite file, created on first run). Restart the server and notes survive.

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | HTML notes frontend |
| GET | `/notes` | JSON array of all notes (newest first) |
| POST | `/notes` | `{title, body}` → created note JSON |
| GET | `/notes/{id}` | Single note |
| DELETE | `/notes/{id}` | `{"ok":true}` |

```sh
curl -X POST http://localhost:48080/notes \
  -d '{"title":"hello","body":"world","id":0,"created":0}'
curl http://localhost:48080/notes
```
