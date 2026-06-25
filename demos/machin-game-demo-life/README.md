# machin-game-demo-life

Conway's Game of Life. TUI cellular automaton on a 78×22 grid.

**Exercises:** 2D grid as flat `[]int`, double-buffer step, `rand_bytes` for random seeding, `byte_at` for byte inspection, `raw_mode`/`read_key` for live control.

## Build & run

```sh
./build.sh
./app          # run in a real terminal (needs 80×24)
```

## Controls

| Key | Action |
|-----|--------|
| Space | Toggle running / paused |
| N | Step one generation (when paused) |
| R | Randomize the grid |
| Q | Quit |

## What it demonstrates

- Flat `[]int` double-buffer for cellular automaton (no slice-of-slices)
- `rand_bytes` + `byte_at` for probabilistic seeding (~25% alive)
- Nested `while` loops over a 2D grid
- ANSI cursor-home frame rendering without flickering
