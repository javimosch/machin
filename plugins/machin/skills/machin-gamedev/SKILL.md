---
name: machin-gamedev
description: Build native games and interactive desktop/terminal apps in machin (MFL) — the canonical setup, build-and-verify workflow, raylib C-FFI surface, audio, and the hard-won caveats/gotchas. Use when writing or debugging a machin game (terminal TUI or raylib GUI), or any machin program that draws a window, plays sound, or reads real-time input. Covers terminal TUI, raylib GUI/audio, 3D cameras, GPU meshes (pointer/array FFI), instancing, shaders, procedural worlds (noise), fixed-timestep sim loops, a pure-MFL math3d module (Vec3), verlet physics (position-based dynamics, constraint relaxation, collision), rlgl point-cloud rendering (RL_POINTS for particle systems and star fields), ballistic physics (gravity + quadratic drag + wind, trajectory prediction), and first-person player controllers (walking, jumping, platform stepping, object pushing, walk/fly toggle). Distilled from machin-game-demo-snake / -2048 / -flappy / -simon / -3d / -terrain / -planet / -cyberpunk / -solar / -physics / -galaxy / -ballistics / -player.
---

# Building games in machin

machin (MFL) compiles to a native binary through C, and reaches real games two ways:

- **Terminal (TUI):** ANSI escapes for drawing + `raw_mode`/`read_key` for input. A no-dependency single binary. → [machin-game-demo-snake](https://github.com/javimosch/machin-game-demo-snake) (Snake).
- **GUI / audio:** [raylib](https://www.raylib.com/) through machin's C **FFI** — a real OpenGL window, textures, sound. Links the system graphics/audio stack, so **not** self-contained. → [machin-game-demo-2048](https://github.com/javimosch/machin-game-demo-2048) (shapes+text), [machin-game-demo-flappy](https://github.com/javimosch/machin-game-demo-flappy) (sprites/textures), [machin-game-demo-simon](https://github.com/javimosch/machin-game-demo-simon) (audio), [machin-game-demo-3d](https://github.com/javimosch/machin-game-demo-3d) (3D + per-object rotation), [machin-game-demo-anim](https://github.com/javimosch/machin-game-demo-anim) (2D procedural), [machin-game-demo-terrain](https://github.com/javimosch/machin-game-demo-terrain) (immediate-mode mesh), [machin-game-demo-planet](https://github.com/javimosch/machin-game-demo-planet) (GPU mesh / pointer FFI), [machin-game-demo-cyberpunk](https://github.com/javimosch/machin-game-demo-cyberpunk) (infinite noise world + fly camera), [machin-game-demo-solar](https://github.com/javimosch/machin-game-demo-solar) (solar-system sim, fixed-timestep, math3d module), [machin-game-demo-physics](https://github.com/javimosch/machin-game-demo-physics) (verlet physics sandbox, constraint relaxation, collision), [machin-game-demo-galaxy](https://github.com/javimosch/machin-game-demo-galaxy) (procedural spiral galaxy, rlgl point cloud), [machin-game-demo-ballistics](https://github.com/javimosch/machin-game-demo-ballistics) (interactive cannon sim, ballistic trajectory prediction, drag + wind), [machin-game-demo-player](https://github.com/javimosch/machin-game-demo-player) (first-person 3D playground, walk/fly toggle, platform stepping, object pushing).

Where this domain is heading: [`docs/NORTH-STAR-GAMEDEV.md`](../../docs/NORTH-STAR-GAMEDEV.md) (tiers + the gap roadmap: pointer/array FFI for real GPU meshes, a vector/matrix layer, a noise builtin, shaders, callbacks).

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
- **Nested cstructs** (machin **v0.45.0**) — a `cstruct` field may be another `cstruct`, marshaled recursively. Declare the inner one **first**. Required for **3D** and 2D cameras: `cstruct Vector3 { x f32 y f32 z f32 }` then `cstruct Camera3D { position Vector3 target Vector3 up Vector3 fovy f32 projection i32 }`; construct with nested literals `Camera3D{Vector3{12.0,7.0,0.0}, Vector3{0.0,1.5,0.0}, Vector3{0.0,1.0,0.0}, 45.0, 0}` and pass by value to `BeginMode3D`.

**3D** (see [machin-game-demo-3d](https://github.com/javimosch/machin-game-demo-3d)): bracket 3D draws with `BeginMode3D(cam)` / `EndMode3D()`; `DrawCube(Vector3, f32,f32,f32, Color)`, `DrawCubeWires`, `DrawSphere(Vector3, f32, Color)`, `DrawGrid(i32, f32)`; any `DrawText` after `EndMode3D` is screen-space. Rebuild the `Camera3D` each frame rather than mutating it. `projection` `0` = `CAMERA_PERSPECTIVE`.

**Per-object transforms (rotation/scale).** `DrawCube`/`DrawSphere` only translate (axis-aligned). To rotate or scale an object, use raylib's immediate-mode matrix stack from **rlgl**: `rlPushMatrix` → `rlTranslatef(x,y,z)` → `rlRotatef(deg, ax,ay,az)` / `rlScalef(x,y,z)` → draw at the **local origin** → `rlPopMatrix`. Those functions live in `rlgl.h`, **not** `raylib.h` — but their symbols are in `libraylib.a`, so declare them in a **headerless extern block** (machin emits the prototypes itself; symbols link from the raylib block's libs). No new machin feature — the existing scalar/`void` FFI carries it:

```
extern "rlgl" { fn rlPushMatrix() fn rlPopMatrix() fn rlTranslatef(f32,f32,f32) fn rlRotatef(f32,f32,f32,f32) fn rlScalef(f32,f32,f32) }
// spin a cube in place:
rlPushMatrix()  rlTranslatef(x,y,z)  rlRotatef(deg, 0.0,1.0,0.0)  DrawCube(v3(0.0,0.0,0.0), s,s,s, c)  rlPopMatrix()
```

(The same stack works in 2D between `BeginDrawing`/`EndDrawing`. The **headerless-extern** trick is general: any `libraylib.a`/system-lib symbol that's scalar/`void` can be reached this way without its header.)

**Procedural meshes (immediate mode)** — see [machin-game-demo-terrain](https://github.com/javimosch/machin-game-demo-terrain). Compute geometry in MFL and stream it triangle-by-triangle with rlgl: `extern "rlgl" { fn rlBegin(i32) fn rlEnd() fn rlColor4ub(u8,u8,u8,u8) fn rlVertex3f(f32,f32,f32) fn rlDisableBackfaceCulling() }`; `rlBegin(4)` (= `RL_TRIANGLES`), one `rlColor4ub` then three `rlVertex3f` per triangle, `rlEnd()` — inside `BeginMode3D`. **Flat-shade in MFL** (no light shader otherwise): face normal = cross of two edges, `sh = 0.4 + 0.6*clamp(dot(n_unit, lightDir),0,1)` (`sqrt` to normalize), multiply color by `sh`. `rlDisableBackfaceCulling()` if the surface is seen from both sides (else winding-dependent triangles vanish → thin sliver). ~10–15k `rlVertex3f`/frame is fine.

**Point clouds via rlgl (RL_POINTS)** — see [machin-game-demo-galaxy](https://github.com/javimosch/machin-game-demo-galaxy). For thousands of individual points (stars, particles, sparks, dust), use `rlBegin(0)` (= `RL_POINTS`) instead of per-item `DrawSphere` calls. The same rlgl extern block applies: `rlBegin(0)`, then per point `rlColor4ub(r,g,b,255)` + `rlVertex3f(x,y,z)`, then `rlEnd()`. This renders the entire set in **one GPU draw call** vs N calls for N `DrawSphere`s. Good for ~5000+ points at 60fps. Brightness/visual size can be faked by emitting **multiple overlapping `rlVertex3f` at the same position** — 1 pass = 1 pixel, 2 passes = 2 pixels (medium bright), 3+ passes = bright/large. Combine with a dark `ClearBackground` (near-black blue: `col(2,2,8)`) for a space/atmosphere look. The headerless extern trick works: `extern "rlgl" { fn rlBegin(i32) fn rlEnd() fn rlColor4ub(u8,u8,u8,u8) fn rlVertex3f(f32,f32,f32) }`.

**GPU meshes / pointer-array FFI** (v0.47.0–v0.48.0). Build C buffers in raw memory and a C struct as a cstruct, then hand them to the GPU. Raw memory (pointers are `int`s): `p := alloc(nbytes)` (zeroed), `poke_f32`/`poke_i32`/`poke_u8`/`poke_u16`/`poke_ptr(p, byteOffset, v)`, `peek_f32`/`peek_i32`, `free(p)`. Pointer params: `ptr` (a raw pointer, `void*` → any `T*`); `*T` (deref a raw pointer, pass the struct by value); **`T*` inout** (pass a cstruct *variable* by pointer, writing the modified struct back). A `cstruct` **field** may be `ptr`, so you declare the struct and the C compiler lays it out. Mesh flow: `alloc`+`poke` the vertex/color arrays, then `mesh := Mesh{vcount, vcount/3, vbuf, cbuf, 0, 0}` with `cstruct Mesh { vertexCount i32 triangleCount i32 vertices ptr colors ptr vaoId u32 vboId ptr }` (field names match raylib's), `UploadMesh(mesh, false)` (inout `Mesh*`; writes vao/vbo back), `LoadModelFromMesh(mesh)` → `Model` (opaque), then `DrawModelEx` each frame. **Build once**, GPU-resident — vs. immediate mode which re-emits every frame. (see machin-game-demo-planet)

**Procedural worlds + infinite terrain + fly camera** — see [machin-game-demo-cyberpunk](https://github.com/javimosch/machin-game-demo-cyberpunk). **Noise** (v0.49.0): `noise2(x,y)`/`noise3(x,y,z)` are deterministic Perlin, ~`[-1,1]`; layer them into **fbm** in MFL (`s += amp*noise2(x*fr,y*fr); amp*=0.5; fr*=2`) for terrain/placement. **Infinite streaming:** a small slot pool — pre-fill `[]Model` with zeroed `Model{}` (a zero opaque handle is a safe no-op for `DrawModel`/`UnloadModel`) + a `cused[]` flag + `ccx/ccz`; each frame unload chunks out of `RAD` and build in-range ones into free slots, so only the leading edge regenerates. Bake **world coords** into each chunk mesh and `DrawModel` at the origin. **Fly camera:** `forward=(cos(pitch)*sin(yaw), sin(pitch), cos(pitch)*cos(yaw))`, `right=normalize(cross(forward,up))=(-fz,0,fx)/len`, driven by `GetMouseDelta` (yaw/pitch) + `IsKeyDown` + `GetFrameTime`; call `DisableCursor()` for mouse-look. raylib takes ownership of an uploaded mesh's `alloc`'d buffers (frees them on `UnloadModel` — don't free yourself). **Budget chunk builds** (≤ a few mesh builds/frame) so flying never hitches the mouse.

**Shaders / post-processing** (composition — no new machin feature; see machin-game-demo-cyberpunk). `Shader` is `{id u32, locs ptr}` (a pointer cstruct field), `RenderTexture2D` is `{id u32, texture Texture2D, depth Texture2D}` (nested cstructs); `LoadShaderFromMemory(vs, fs)` (GLSL as `\n`-escaped single-line strings; standard names `vertexPosition`/`vertexTexCoord`/`vertexColor`/`mvp`/`texture0`), `SetShaderValue(sh, loc, ptr, kind)` (`kind` 0/1/2 = float/vec2/vec3, value from a small `alloc`'d buffer), `SetShaderValueTexture` (bind a sampler). Post-process: `BeginTextureMode(rt)` → draw scene → `EndTextureMode`, then `BeginShaderMode(sh)` → `DrawTextureRec(rt.texture, Rectangle{0,0,W,-H}, ...)` (**negative height flips** the render target) → `EndShaderMode`. **Depth fog:** linearize `texture(depthTex,uv).r` with near/far and skip `depth>0.9995` (sky).

**GPU instancing** (thousands of meshes in one `DrawMeshInstanced` — also composition; flora in machin-game-demo-cyberpunk). `Material` is a partial cstruct `{ shader Shader, maps ptr }` (the C `params[4]` array can't be a cstruct field, but it's left zeroed by marshaling — fine). Instancing VS declares `in mat4 instanceTransform` (`gl_Position = mvp*instanceTransform*vec4(pos,1)`). **raylib 5.0 has no `SHADER_LOC_VERTEX_INSTANCE_TX`** — it uses `SHADER_LOC_MATRIX_MODEL` (index **9**) for the instance attribute: `poke_i32(ish.locs, 36, GetShaderLocationAttrib(ish, "instanceTransform"))`. `md := LoadMaterialDefault(); imat := Material{ish, md.maps}`. Build the per-instance `Matrix[]` in raw memory: **translation @ byte 12/28/44, scale @ 0/20/40, 1.0 @ 60**, rest zero. Keep positions deterministic (world grid + `noise2`) so instances don't flicker. **Math:** machin has **native** math builtins (v0.46.0) — `sin cos tan asin acos atan atan2 sqrt cbrt pow exp log log2 log10 floor ceil round trunc abs fmod hypot` and `pi()` (numeric in, `float` out; `-lm` linked only when used). So an orbit is just `v3(R*cos(a), h, R*sin(a))`. (An `extern "m"` of the same name still shadows the builtin if you want a specific libm signature.)

**Procedural skeletal animation** (fauna in machin-game-demo-cyberpunk — composition over the rlgl matrix stack, no new feature). Pose a bone hierarchy with **forward kinematics**: nest `rlPushMatrix`/`rlTranslatef`(to the joint)/`rlRotatef`(the joint angle)/draw-at-local-origin/`rlPopMatrix`. A leg is two segments — translate to the hip, rotate the whole leg, `DrawCube` the upper segment, `rlTranslatef` down to the knee, `rlRotatef` the shin, `DrawCube` the lower segment. Drive a **diagonal gait** from `sin`/`cos(t*speed)`: legs 0&3 share one phase, 1&2 the opposite (`+π`); `swing = sin(phase)*A` rotates the hip, a `lift` term (`cos(phase)` clamped `>0`) bends the knee on the forward stroke, and a body bob (`sin(t*speed*2)*h`) sells the weight shift. Wrap the whole creature in one more `rlPush`/`rlTranslatef`(world pos)/`rlRotatef`(heading)/`rlPop`. Snap to terrain height each frame. No skinned mesh needed — nested transforms over primitives carry it.

**Pushing the draw distance past raylib's ~1 km clip.** raylib's auto perspective uses a fixed far plane (`rlSetClipPlanes` does **not** exist in 5.0). Override it with **`rlSetMatrixProjection`** (rlgl), passing a `Matrix` **by value** — declare `cstruct Matrix { m0 f32 m4 f32 m8 f32 m12 f32 m1 f32 m5 f32 m9 f32 m13 f32 m2 f32 m6 f32 m10 f32 m14 f32 m3 f32 m7 f32 m11 f32 m15 f32 }` (raylib's field order) and build a perspective matrix with your own far plane: `m0=f/aspect, m5=f` (`f=1/tan(fovy/2)`), `m10=-(far+near)/(far-near)`, `m14=-(2·far·near)/(far-near)`, `m11=-1`, rest 0. Call it **every frame right after `BeginMode3D`** (it flushes the batch and sets the projection). Gotcha: a `*Matrix` deref-param in a **headerless** extern emits invalid C (`extern void f(*Matrix)`) — pass the struct **by value** (`fn rlSetMatrixProjection(Matrix)`) instead; the cstruct must be declared in a **headered** block so the C type resolves. For a believable far horizon, pair it with a **coarse LOD underlay** — one big low-res terrain mesh recentered on the camera as it drifts a snap cell — under the fine chunks, the seam hidden by fog.

Sprite tricks: a **sprite sheet** is one PNG; pick a frame with a source `Rectangle{float(frame*48), 0, 48, 48}` and rotate via `DrawTexturePro` around `origin = Vector2{w/2, h/2}`. **Flip** with a negative source height (`Rectangle{0,0,88,-600}`) — reuse one texture for both orientations. **Center text** with `MeasureText`.

raylib codes used by the games: keys `SPACE 32`, arrows `LEFT 263 RIGHT 262 UP 265 DOWN 264`, `W 87 A 65 S 83 D 68`, digits `1..4 = 49..52`, `R 82`; mouse button `left = 0`. Esc is raylib's default window-close key (caught by `WindowShouldClose()`).

**Verlet physics (position-based dynamics) — pure MFL, no C library.** The solar-system sim drove the math module; the physics demo ([machin-game-demo-physics](https://github.com/javimosch/machin-game-demo-physics)) drives the simulation layer on top of it. Core patterns:

- **Implicit velocity via `old_pos`.** A `Particle` stores `pos` (current) and `old` (previous frame). Velocity = `pos - old`. Integration per substep: `new_pos = pos + (pos-old)*damping + gravity*dt²`. No Euler/Verlet distinction — one formula. (Matches MFL's value-semantic structs: you return a new `[]Particle` each tick — fine for ~100–300 particles.)
- **Distance constraint relaxation.** A constraint holds two particle indices + a rest length. Each iteration projects both particles along the inter-particle axis: `delta = p2-p1; dist=len(delta); diff=(dist-rest_len)/(dist+ε); off=delta*diff*0.5; if !pinned: p1+=off; p2-=off`. Run 3–5 iterations per substep for stable convergence. The `*0.5` splits the correction equally (weight by inverse mass for non-uniform masses).
- **Substeps decouple speed from stability.** The physics tick is always `1/60`; divide into 6–8 substeps of `dt/6`. Each substep runs integrate → constraint iterations → collision, so objects that move fast per frame don't tunnel.
- **O(n²) sphere-sphere collision** is fine for ≤200 particles (~40k distance checks/substep). Check `len(pos[j]-pos[i]) < r_i+r_j`; push apart along the normalized delta by half the overlap each. Include the `ε = 0.0000001` to avoid division by zero when particles are exactly coincident.
- **Ground collision** is a one-liner: `if pos.y < radius { pos.y = radius; old.y = pos.y + (old.y-pos.y)*bounce }` where `bounce=0.4` kills most of the rebound velocity.
- **Velocity coloring** for live diagnostics: compute `speed = len(pos-old)/dt` and map it through a gradient (cold blue → cyan → yellow → hot red). One function, visualized instantly across all particles.

**Ballistic physics (projectile simulation) — gravity + drag + wind.** The ballistics demo ([machin-game-demo-ballistics](https://github.com/javimosch/machin-game-demo-ballistics)) adds the projectile layer for Tier 4 combat sims. Core patterns:

- **Euler integration at a fixed timestep.** `1/60s` per step: `v += a·dt; x += v·dt`. Three forces: gravity (constant downward), quadratic drag (`-drag_coeff · |v| · v` — realistic because it scales with velocity squared), and wind (constant horizontal). The same integrator is used for both the **predicted trajectory** (computed ahead of time, drawn as dots) and the **live projectile** (frame-by-frame during flight). This makes the prediction trustworthy — it IS the simulation.
- **Phase-state machine for interactive flow.** A single `phase` int (0=aiming, 1=firing, 2=impact) controls which inputs are read and what is drawn. Phase 0 reads arrows (angle/power) + SPACE (fire). Phase 1 advances the projectile and appends to a trail slice. Phase 2 shows hit/miss feedback with a countdown timer, then auto-resets to 0.
- **Trajectory prediction with `compute_traj()`.** A function that runs the full integrator for up to 500 steps and returns `([]Vec3, steps)`. Called every frame during aiming. The result is rendered sparsely (every 3rd step, dim sphere) to show the arc without visual clutter.
- **Hit detection with radial check.** On ground impact (`y < 0`), compute `sqrt((px-tx)² + (pz-tz)²)` and compare to the target radius. Display "HIT" or "MISS by Xm" (the distance computed from the impact position).
- **Smooth arrow-key controls.** Angle adjusts by `40°·dt` per frame (frame-rate independent), clamped to `[5°,85°]`. Power adjusts by `20·dt`, clamped to `[4,50]`. Using `IsKeyDown` for held-arrow adjustment and `IsKeyPressed` for the single-fire trigger (SPACE) keeps the controls feeling responsive.
- **Trail as a growing slice.** Each frame during phase 1, `trail = append(trail, pos)`. The trail is drawn as small spheres (dimmer toward the start). Capped at 500 entries to prevent unbounded memory growth.

**First-person player controller — walking, jumping, pushing.** The player demo ([machin-game-demo-player](https://github.com/javimosch/machin-game-demo-player)) adds the character controller that every interactive 3D game needs. Core patterns:

- **Movement from camera yaw.** Horizontal movement direction is computed from `sin(yaw)` / `cos(yaw)` — the same `yaw` that controls mouse look. W always moves where the player is looking, even when pitched up/down. Compute the 2D direction vector, normalize, scale by speed.
- **Smooth acceleration.** Instead of `vel = input_dir` (instant), interpolate toward it: `vel += (input - vel) * min(accel * dt, 1)`. For `accel=12`, this reaches 90% of target speed in ~0.2s. When no input, `vel` decays via friction `vel *= (1 - friction*dt*10)`. This gives natural-feeling movement with no explicit state machine.
- **Gravity + ground collision per frame.** `pvy += GRAVITY*dt; pos += vel*dt`. After integration, if `foot_y < 0`, snap to 0, zero `pvy`, set `grounded=1`, apply horizontal friction. This runs before platform checks.
- **Platform stepping.** For each platform, test `if foot_xz is within platform_xz bounds AND foot_y is near platform_top`: snap `foot` to platform top + eye height, set grounded. This naturally handles stepping up and walking off edges. Multiple platforms require iterating the list each frame and testing each one.
- **Walk/fly mode toggle.** A single int `walk_mode` (0/1) and F key flips it. In walk mode, the player controller runs. In fly mode, the free camera from solar operates. On transition to walk, transfer the fly camera's position/orientation to the player — fly to a spot, press F, land there.
- **Object pushing with collision physics.** Sphere-sphere collision between player and each object. The overlap is split: both player and object are pushed apart by half the overlap. Objects have their own gravity + ground collision + friction (separate from the player's). Multiple objects can be pushed simultaneously.

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
- **`a < -b` is a lexer trap.** `encode` tightens `< -` to `<-`, which lexes as the channel-receive token (`expected "{", got "<-"`). Write `a < 0.0 - b` or flip the comparison. (Common with negative thresholds: `if h < -0.7` → `if h < 0.0 - 0.7`.)
- **Random:** there's no PRNG builtin; `byte_at(rand_bytes(1), 0) % N` picks a value (a second byte `< 26` ≈ a 10% branch).
- **Frame timing:** immediate-mode means never `sleep` mid-game (it freezes the window). Drive animations/state with a per-frame tick counter and `SetTargetFPS`. (Terminal games *do* `sleep(ms)` per tick — they own the loop.)
- **Mutating shared state:** slices are reference-ish — `f(board)` then `board[i] = v` is visible to the caller (return only summaries). Structs are value types (a copy), so a function can't mutate a caller's struct.
- **cstruct types CANNOT be fields in MFL `type` structs.** A `cstruct` declared in an `extern` block (`Model`, `Color`, `Sound`, `Shader`, …) can be a local variable, passed to functions, or stored in a **slice** (`[]Model`, `[]Color`), but it **cannot** be a field of an MFL `type` struct. The C type map isn't available at the point the MFL struct's typedef generates. Workaround: use **parallel slices** — `bodies := []Body{}; models := []Model{}` — and index them together. (Discovered by machin-game-demo-solar.)
- **Non-empty `[]struct` literals are not supported.** `xs := []S{a, b, c}` fails; build with `append`: `xs := []S{}; xs = append(xs, a); xs = append(xs, b); …`. (Empty `[]S{}` is fine.)

## Each game drove a feature (the dogfood record)

| game | exercises | drove into machin |
|------|-----------|-------------------|
| snake | terminal real-time input | `raw_mode` / `read_key` (v0.41.0) |
| 2048 | raylib FFI: scalars + `Color` | (composed; no new builtin) |
| flappy | textures/sprites, `f32` structs, struct-return | `float()` int→float (v0.43.0) |
| simon | audio: pointer-bearing `Sound` by value | FFI **opaque handles** `cstruct Name {}` (v0.44.0) |
| 3d demo | 3D: `Camera3D` (struct of `Vector3`s); per-object rotation (rlgl matrix stack, headerless extern) | FFI **nested cstructs** (v0.45.0); its libm orbit then drove **native math** builtins (v0.46.0); rotation = composition (no new feature) |
| anim | 2D procedural flow field (sin/cos/atan2/hypot over time) | composition on native math + 2D FFI (no new feature) |
| terrain | procedural mesh: per-vertex heights, flat-shaded, streamed via rlgl immediate mode | composition (no new feature); points at the **pointer/array FFI** gap for real GPU VBOs |
| planet | static **GPU mesh**: vertex/color arrays in raw memory, `Mesh` as a cstruct, upload to VRAM | **pointer/array FFI** (v0.47.0): raw memory `alloc`/`poke_*`; then **pointer cstruct fields + inout `T*`** (v0.48.0) dropped the hard-coded offsets |
| cyberpunk | **infinite** procedural world: fbm-noise terrain in GPU-mesh chunks, fly camera, grimy city districts, **GPU-instanced flora**, **shader depth fog**, **skeletal fauna**, **10 km draw distance** | **`noise2`/`noise3`** Perlin (v0.49.0); buildings/instancing/shaders/fog/skeleton-animation/far-clip all composition (rlgl matrix stack + by-value `Matrix` cstruct) |
| solar | **3D solar-system sim**: 7 noise3-textured GPU-mesh planets, **fixed-timestep** orbital sim (60 Hz accumulator), 6DOF fly camera, **pure-MFL math3d module** (`Vec3` add/sub/scale/dot/cross/len/norm/lerp/dist) | composition (no new builtin); drives the **vector/math layer** (feature #2) and **fixed-timestep sim** (feature #6) from the north star; surfaced the cstruct-in-struct-field + slice-literal caveats |
| physics | **verlet physics sandbox**: ~100 particles falling/colliding/stacking, chain pendulum, velocity coloring, noise3-based random scene population | composition (no new builtin); first constraint-based physics in pure MFL — verlet integration, distance constraint relaxation (iterations × substeps), O(n²) sphere-sphere collision, all over the `Vec3` module; drives Tier 4 physics patterns |
| galaxy | **procedural spiral galaxy**: ~5000 stars in 4 logarithmic spiral arms (`θ = k·log(r)`), spectral OBAFGKM colors, rlgl point-cloud render (RL_POINTS, one batch), slow rotation, core bulge | composition (no new builtin); first demo to use **rlgl point rendering** (`rlBegin(0)` / `rlColor4ub` / `rlVertex3f` / `rlEnd()`); demonstrates procedural star placement, weighted spectral distribution, multi-pass point sizing, and the `v3_rot_y()` helper |
| ballistics | **interactive cannon sim**: trajectory prediction with gravity + quadratic drag + wind, aim/power controls, live projectile with trail, hit/miss detection on a target | composition (no new builtin); demonstrates ballistic Euler integration, the prediction-vs-execution pattern (same integrator for both), phase-state machine (aim→fire→impact), interactive HUD with real-time angle/power feedback, and `IsKeyPressed` for single-fire controls |
| player | **first-person 3D playground**: walking character with smooth acceleration, platform stepping (4 heights), 5 pushable spheres with gravity physics, walk/fly camera toggle (F) | composition (no new builtin); demonstrates the first-person player controller layer — WASD movement relative to camera yaw, smooth acceleration `vel += (input - vel)·min(accel·dt,1)`, gravity + ground + platform collision, walk/fly mode toggle, pushable-object physics (sphere-sphere collision with player, object gravity + friction), crosshair + HUD in screen space |

When a new game hits a wall, that's the point: fill the gap in the language, release, and note it here.
