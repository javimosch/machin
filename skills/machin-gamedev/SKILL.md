---
name: machin-gamedev
description: Build native games and interactive desktop/terminal apps in machin (MFL) — the canonical setup, build-and-verify workflow, raylib C-FFI surface, audio, and the hard-won caveats/gotchas. Use when writing or debugging a machin game (terminal TUI or raylib GUI), or any machin program that draws a window, plays sound, or reads real-time input. Distilled from machin-game-snake / -2048 / -flappy / -simon.
---

# Building games in machin

machin (MFL) compiles to a native binary through C, and reaches real games two ways:

- **Terminal (TUI):** ANSI escapes for drawing + `raw_mode`/`read_key` for input. A no-dependency single binary. → [machin-game-snake](https://github.com/javimosch/machin-game-snake) (Snake).
- **GUI / audio:** [raylib](https://www.raylib.com/) through machin's C **FFI** — a real OpenGL window, textures, sound. Links the system graphics/audio stack, so **not** self-contained. → [machin-game-2048](https://github.com/javimosch/machin-game-2048) (shapes+text), [machin-game-flappy](https://github.com/javimosch/machin-game-flappy) (sprites/textures), [machin-game-simon](https://github.com/javimosch/machin-game-simon) (audio).

Each game is its own public repo with a `build.sh` (`machin encode src.src > app.mfl && machin build app.mfl -o app`), a `README.md`, a repo-root `SKILL.md`, and committed `assets/`. This skill is the shared substrate; each game's SKILL.md has its specifics.

> Run `machin guide` for the version-exact language surface (builtins, idioms, gotchas). This skill is about *games* specifically.

## Build & verify workflow (this environment)

- **raylib, no root:** there's no system raylib and no passwordless sudo. The game `build.sh` auto-vendors raylib's prebuilt **static** release (`raylib-5.0_linux_amd64.tar.gz`) into `vendor/`, then injects `cflags "-I.../include -L.../lib"` + `link ":libraylib.a"` into a *throwaway* `.mfl` so the committed source stays system-style (`link "raylib"`). A copy is cached at `/tmp/rl/raylib-5.0_linux_amd64`. `build.sh` prefers a system raylib (`pkg-config --exists raylib`) when present.
- **Display + audio are live:** `DISPLAY=:0`, PulseAudio works. Run a game backgrounded, `sleep`, then `kill` it.
- **Screenshot to verify rendering:** `DISPLAY=:0 import -window root /tmp/shot.png` (ImageMagick). `scrot`/`maim`/`grim`/`xdotool` are NOT installed. It captures the whole root (1920×1080); the game window is inside it. Read the PNG back to confirm the frame.
- **Can't inject keystrokes** (no xdotool). Verify *gameplay* with (a) a temporary **autopilot** build — `sed` the input line to a rule like `flap := bird_y > 320.0` and start in the playing state — screenshot mid-run; or (b) **headless logic tests** — factor the pure logic (slide/merge, collision) and run it with `machin run`, asserting output. Do the logic test first; it's faster and catches the type errors.
- **Assets load by relative path** → run the binary from the repo root (`./app`, not from `/tmp`).
- **`machin encode` runs the typechecker**, so most errors show at encode time (no `cc` needed). `machin build x.mfl --emit-c` prints the generated C when you need to see the FFI marshaling.

## The raylib FFI surface

One `extern "raylib" { … }` block is the entire C boundary; everything else is pure MFL. Template (declare only what you call):

```
extern "raylib" {
    header "raylib.h"
    link "raylib" link "GL" link "m" link "pthread" link "dl" link "rt" link "X11"
    cstruct Color { r u8 g u8 b u8 a u8 }
    cstruct Texture2D { id u32 width i32 height i32 mipmaps i32 format i32 }  // all-int handle
    cstruct Rectangle { x f32 y f32 width f32 height f32 }                    // f32 fields
    cstruct Vector2 { x f32 y f32 }
    cstruct Sound {}                                                          // OPAQUE handle (v0.44.0)
    fn InitWindow(i32, i32, string)   fn SetTargetFPS(i32)   fn WindowShouldClose() bool
    fn BeginDrawing()   fn EndDrawing()   fn ClearBackground(Color)   fn CloseWindow()
    fn DrawRectangle(i32, i32, i32, i32, Color)   fn DrawCircle(i32, i32, f32, Color)
    fn DrawText(string, i32, i32, i32, Color)     fn MeasureText(string, i32) i32
    fn LoadTexture(string) Texture2D              fn DrawTexture(Texture2D, i32, i32, Color)
    fn DrawTextureRec(Texture2D, Rectangle, Vector2, Color)
    fn DrawTexturePro(Texture2D, Rectangle, Rectangle, Vector2, f32, Color)
    fn IsKeyPressed(i32) bool   fn IsMouseButtonPressed(i32) bool   fn GetMouseX() i32  fn GetMouseY() i32
    fn InitAudioDevice()   fn LoadSound(string) Sound   fn PlaySound(Sound)   fn CloseAudioDevice()
}
```

FFI tiers, all verified working:
- **Scalars + by-value structs** (`Color`, and `f32`-field `Rectangle`/`Vector2`) passed by value.
- **Struct return** — `LoadTexture` returns `Texture2D` (an all-int handle) by value.
- **Opaque handles** (`cstruct Sound {}`, machin **v0.44.0**) — a by-value C struct that contains pointers (`Sound`/`Music`/`Font`). machin holds the real C struct and passes it back to fns without naming its fields. Receive it from a fn, store it (a var **or** `[]Sound`), pass it on; **no** construct or `.field`. (A single pointer is simpler: the `ptr` FFI type, an `int`.)

Sprite tricks: a **sprite sheet** is one PNG; pick a frame with a source `Rectangle{float(frame*48), 0, 48, 48}` and rotate via `DrawTexturePro` around `origin = Vector2{w/2, h/2}`. **Flip** with a negative source height (`Rectangle{0,0,88,-600}`) — reuse one texture for both orientations. **Center text** with `MeasureText`.

raylib codes used by the games: keys `SPACE 32`, arrows `LEFT 263 RIGHT 262 UP 265 DOWN 264`, `W 87 A 65 S 83 D 68`, digits `1..4 = 49..52`, `R 82`; mouse button `left = 0`. Esc is raylib's default window-close key (caught by `WindowShouldClose()`).

## THE gotcha: no implicit int→float

This bit every game. MFL does **not** implicitly convert `int`→`float`. Only a *flexible numeric literal* (`5`, `560`) promotes on contact with a float. A **concrete** int does **not**, and mixing it with a float is a hard `int vs float` compile error. Concrete ints come from: **a function return** (even `func GROUND_Y() { n = 560 }`), `byte_at`, `len`, a **typed parameter**, an **`int`-slice element**, and an `f32`/`f64` **cstruct field** also won't take a concrete int.

Fixes (need `float()`, machin **v0.43.0**; `int()` goes the other way):
- Make world-coordinate constants **floats**: `func GROUND_Y() { n = 560.0 }`.
- Wrap concrete-int math entering float: `160.0 + float((byte_at(r,0) << 8 | byte_at(r,1)) % 260)`.
- Concrete int into an `f32` field: `Rectangle{float(frame*48), 0, 48, 48}` (literals like `0`/`48` are fine).
- Keep loop indices pure `int`: use a float **accumulator** (`x = x + SPACING()`), never `i * SPACING()` — multiplying the index by a float drags it to float and then `arr[i]` breaks.
- Going to an FFI `i32` arg from a float: `int(GROUND_Y())`.

Rule of thumb: keep each value in one numeric world; cross the boundary explicitly with `float(x)` / `int(x)`.

## Terminal (TUI) games

- **Real-time input** (machin **v0.41.0**): `raw_mode(1)` puts the tty in cbreak/no-echo; `read_key()` is a non-blocking single-key read (`""` if nothing waiting). Always `raw_mode(0)` before exit. `input()` is line-buffered and unusable for games.
- **ANSI without `\x`:** MFL strings have no `\x1b`. Build ESC from hex: `ESC := bytes_str(from_hex("1b"))`, then `ESC+"[2J"` (clear), `ESC+"[H"` (home), `ESC+"[?25l"`/`[?25h"` (hide/show cursor), `ESC+"["+str(n)+"m"` (color).
- **One `print` per frame** (build the whole frame string, then `flush()`); per-cell printing flickers.
- Under a pipe/CI, `raw_mode` no-ops and `read_key` falls back to a `select` poll — handy for a smoke test (snake runs straight into the wall and exits).

## Other caveats

- **A GUI binary is not self-contained** — it links `libGL`/`libX11`/raylib/audio and needs a display (and an audio device for sound). machin's no-dependency-binary property holds for the headless domain only. Say so in the README.
- **`str(bool)` works** as of machin **v0.42.0** (`"true"`/`"false"`); on older compilers it was a type error, so keep bools in control flow there.
- **No slice ranges** (`s[1:]` doesn't parse) — rebuild with a loop (e.g. snake's `drop_first`).
- **Random:** there's no PRNG builtin; `byte_at(rand_bytes(1), 0) % N` picks a value (a second byte `< 26` ≈ a 10% branch).
- **Frame timing:** immediate-mode means never `sleep` mid-game (it freezes the window). Drive animations/state with a per-frame tick counter and `SetTargetFPS`. (Terminal games *do* `sleep(ms)` per tick — they own the loop.)
- **Mutating shared state:** slices are reference-ish — `f(board)` then `board[i] = v` is visible to the caller (return only summaries). Structs are value types (a copy), so a function can't mutate a caller's struct.

## Each game drove a feature (the dogfood record)

| game | exercises | drove into machin |
|------|-----------|-------------------|
| snake | terminal real-time input | `raw_mode` / `read_key` (v0.41.0) |
| 2048 | raylib FFI: scalars + `Color` | (composed; no new builtin) |
| flappy | textures/sprites, `f32` structs, struct-return | `float()` int→float (v0.43.0) |
| simon | audio: pointer-bearing `Sound` by value | FFI **opaque handles** `cstruct Name {}` (v0.44.0) |

When a new game hits a wall, that's the point: fill the gap in the language, release, and note it here.
