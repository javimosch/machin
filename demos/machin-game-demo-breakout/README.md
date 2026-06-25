# machin-game-demo-breakout

Breakout clone. TUI, 80×24 terminal, 5 rows × 10 columns of bricks.

**Exercises:** float ball physics, brick collision detection on a flat `[]int` grid, paddle spin, life system, game-over/win screens.

## Build & run

```sh
./build.sh
./app          # run in a real terminal (needs 80×24)
```

## Controls

| Key | Action |
|-----|--------|
| A | Move paddle left |
| D | Move paddle right |
| Q | Quit |

Clear all 50 bricks to win. You have 3 lives.

## What it demonstrates

- Float ball bouncing with brick collision grid indexing
- Brick `[]int` array (1=alive, 0=dead) indexed by `(int(ball_y)-2)*10 + (int(ball_x)-5)/7`
- Paddle spin: hit angle varies by distance from paddle center
- Life/death cycle: reset ball position on bottom miss
- Win/lose screens via frame string
