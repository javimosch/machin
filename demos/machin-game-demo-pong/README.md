# machin-game-demo-pong

Single-player Pong against an AI opponent. TUI game using ANSI escapes.

**Exercises:** float ball physics, screen-buffer TUI pattern, `raw_mode`/`read_key`, global float state, `int()`/`float()` conversions.

## Build & run

```sh
./build.sh
./app          # run in a real terminal (needs raw mode)
```

Requires an 80×24 terminal.

## Controls

| Key | Action |
|-----|--------|
| W | Move paddle up |
| S | Move paddle down |
| Q | Quit |

## What it demonstrates

- Float-precision ball physics (velocity ~1.5 cells/tick at 40ms/tick)
- AI opponent tracks ball Y position
- Paddle spin on off-center hits
- Full screen buffer drawn each frame via `print` + `flush`
- `raw_mode(1)` / `read_key()` for non-blocking input
