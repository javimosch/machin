# machin-game-demo-dungeon

Simple roguelike dungeon crawler. TUI, hardcoded 40×20 map.

**Exercises:** 2D tile map as flat `[]int`, player movement with wall collision, bump-attack goblins, gold collection, ANSI TUI with status messages.

## Build & run

```sh
./build.sh
./app          # run in a real terminal (needs 80×24)
```

## Controls

| Key | Action |
|-----|--------|
| W | Move up |
| S | Move down |
| A | Move left |
| D | Move right |
| Q | Quit |

Bump into a goblin (`G`) to kill it. Walk onto gold (`$`) to collect it.

## What it demonstrates

- Hardcoded ASCII map parsed at startup into a flat `[]int` tile array
- `charat(row, x)` for string-by-character parsing
- Tile-based movement logic with bump interactions
- Dynamic status messages rendered each frame
