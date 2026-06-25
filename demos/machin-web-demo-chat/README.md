# machin-web-demo-chat

Polling chat server built with [machweb](../../framework/machweb.src).

**Exercises:** global `[]T` slice, JSON serialization, polling-based real-time chat, concurrent goroutine requests.

## Build & run

```sh
./build.sh
./app          # → http://localhost:48080
```

Open the page in two browser tabs to chat between them. Messages are stored in-memory (restart clears them).

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | HTML chat frontend (polls `/msgs/{since}` every second) |
| POST | `/post` | Publish `{from, text}` → `{"ok":true,"n":<total>}` |
| GET | `/msgs/{since}` | Messages since index → `{from, msgs:[...]}` |
| GET | `/msgs` | All messages |
