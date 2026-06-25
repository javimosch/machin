# machin-web-demo-todo

Full-stack TODO list app built with [machweb](../../framework/machweb.src).

**Exercises:** global `[]T` slice, `parse`/`json` round-trips, inline HTML frontend, reactive forms, CRUD over REST.

## Build & run

```sh
./build.sh
./app          # → http://localhost:48080
```

Open the browser to create, toggle, and delete todos. State is in-memory.

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | HTML single-page frontend |
| GET | `/todos` | JSON array of all todos |
| POST | `/todos` | Create todo from `{title, done}` → created todo |
| GET | `/todos/{id}` | Single todo |
| PUT | `/todos/{id}` | Update todo → updated todo |
| DELETE | `/todos/{id}` | Delete → `{"ok":true}` |
