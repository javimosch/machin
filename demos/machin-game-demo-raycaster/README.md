# machin-game-demo-raycaster

ASCII raycaster (Wolfenstein-style). TUI, 80×24 terminal.

**Exercises:** DDA ray-casting algorithm, `sin`/`cos`/`abs` math builtins, float player position/angle, wall-hit distance rendering, minimap strip.

## Build & run

```sh
./build.sh
./app          # run in a real terminal (needs 80×24)
```

## Controls

| Key | Action |
|-----|--------|
| W | Move forward |
| S | Move backward |
| A | Turn left |
| D | Turn right |
| Q | Quit |

## What it demonstrates

- DDA (Digital Differential Analysis) ray-casting: marches grid until wall hit
- Perpendicular distance for fisheye correction
- ASCII wall shading by distance and wall axis (x-walls brighter than y-walls)
- 16×16 map encoded as flat `[]int` (256 individual appends — no slice literal syntax)
- `sin`/`cos` native math builtins for direction vector from angle
- Row-23 minimap strip showing current map slice at player Y
- `float()` conversions at every int→float boundary (MFL enforces this)
