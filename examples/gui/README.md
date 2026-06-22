# GUI example — a raylib game menu

`game_menu.mfl` is a native desktop GUI (a clickable Start / Settings / Exit
menu) drawn with [raylib](https://www.raylib.com/) through machin's C FFI. It
shows that the FFI (scalars + by-value structs — `Color` here) is enough to drive
a real graphics library: open an OpenGL window, draw rectangles and text, and
poll the mouse each frame.

```
+--------------------------------+
|        MFL GAME MENU           |
|     +--------------------+     |
|     |       Start        |     |   <- hover highlights, left-click prints
|     +--------------------+     |
|     |      Settings      |     |
|     +--------------------+     |
|     |        Exit        |     |   <- click (or close the window) to quit
|     +--------------------+     |
+--------------------------------+
```

## Build & run

It is **not** a no-dependency binary — a native GUI links the system graphics
stack (raylib + `libGL`/`libX11`) and needs a display, exactly like a raylib app
in C/Rust/Zig. machin's self-contained-binary property holds for the headless
domain, not for GUI.

**With raylib installed system-wide** (the committed example assumes this):

```bash
sudo apt-get install -y libraylib-dev      # Debian/Ubuntu
machin build examples/gui/game_menu.mfl -o game_menu
./game_menu
```

**Without root** — use raylib's prebuilt static release and point the `extern`
block at it. Download `raylib-5.0_linux_amd64.tar.gz` from the
[raylib releases](https://github.com/raysan5/raylib/releases), then edit the
`extern "raylib"` block's directives to:

```
cflags "-I/path/to/raylib/include -L/path/to/raylib/lib"
link ":libraylib.a"     # force the static archive over the shared .so
```

(The other `link` lines — `GL m pthread dl rt X11` — stay; they are raylib's
transitive dependencies, in link order.)

## How it maps to the FFI

| MFL | raylib C | FFI feature |
|-----|----------|-------------|
| `cstruct Color { r u8 ... }` | `Color { unsigned char r,g,b,a; }` | by-value struct (Phase 2) |
| `fn DrawText(string,i32,i32,i32,Color)` | `void DrawText(const char*,int,int,int,Color)` | scalars + struct arg |
| `fn IsMouseButtonPressed(i32) bool` | `bool IsMouseButtonPressed(int)` | scalar return |
| `link "raylib" link "GL" ...` | `-lraylib -lGL ...` (in order) | multi-lib linking |

raylib is immediate-mode and polls input via functions, so this needs no opaque
handles (Phase 3) or callbacks (Phase 4) — Phases 1–2 of the FFI suffice.
